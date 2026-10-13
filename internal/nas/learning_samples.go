package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const proposalSchema = `
CREATE TABLE IF NOT EXISTS learning_samples(seq INTEGER PRIMARY KEY AUTOINCREMENT,sample_id TEXT UNIQUE NOT NULL,body TEXT NOT NULL);
CREATE TRIGGER IF NOT EXISTS learning_immutable BEFORE UPDATE ON learning_samples BEGIN SELECT RAISE(ABORT,'immutable_sample'); END;
CREATE TABLE IF NOT EXISTS learning_scans(sample_id TEXT PRIMARY KEY,job_version INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS learning_no_delete BEFORE DELETE ON learning_samples BEGIN SELECT RAISE(ABORT,'immutable_sample'); END;
CREATE TABLE IF NOT EXISTS rule_proposals(id TEXT NOT NULL,revision INTEGER NOT NULL,body TEXT NOT NULL,state TEXT NOT NULL,version INTEGER NOT NULL,PRIMARY KEY(id,revision));
CREATE TABLE IF NOT EXISTS proposal_receipts(id TEXT NOT NULL,revision INTEGER NOT NULL,request_digest TEXT NOT NULL,PRIMARY KEY(id,revision));
CREATE TABLE IF NOT EXISTS proposal_sets(set_digest TEXT PRIMARY KEY,proposal_id TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS proposal_audit(seq INTEGER PRIMARY KEY AUTOINCREMENT,proposal_id TEXT NOT NULL,revision INTEGER NOT NULL,kind TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS proposal_no_delete BEFORE DELETE ON rule_proposals BEGIN SELECT RAISE(ABORT,'immutable_proposal'); END;
CREATE TRIGGER IF NOT EXISTS proposal_body_immutable BEFORE UPDATE OF body ON rule_proposals BEGIN SELECT RAISE(ABORT,'immutable_proposal'); END;
`

// SampleRef 绑定原人工意图和允许导出的上下文；规则变化不能修改原样本。
type SampleRef struct {
	SampleID     string `json:"sample_id"`
	Digest       string `json:"digest"`
	ExportDigest string `json:"export_digest"`
}

// LearningContext 只含允许的标题和订阅标识，绝不包含GUID或URL。
type LearningContext struct {
	Subscription string `json:"subscription"`
	Title        string `json:"title"`
}

// LearningSample 是授权最小成功投影，不携带完整manifest、绝对根或凭据。
type LearningSample struct {
	Protocol       int                `json:"protocol"`
	Ref            SampleRef          `json:"ref"`
	JobID          string             `json:"job_id"`
	JobVersion     int                `json:"job_version"`
	PlanVersion    int                `json:"plan_version"`
	ContextVersion int64              `json:"context_version"`
	ParserVersion  int                `json:"parser_version"`
	Changes        []ComparisonChange `json:"changes"`
	Contexts       []LearningContext  `json:"contexts"`
}

// LearningEntry 的seq在适格后才分配，早先未完成记录以后完成不会漏过cursor。
type LearningEntry struct {
	Seq    int64          `json:"seq"`
	Sample LearningSample `json:"sample"`
}

// LearningPage 可有界重放；无适格样本不会触发模型。
type LearningPage struct {
	Protocol int             `json:"protocol"`
	Items    []LearningEntry `json:"items"`
	Next     int64           `json:"next_cursor"`
}

type proposalQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// learningText 对来源数据采取保守拒绝；不尝试猜测或裁剪含秘密的片段。
func learningText(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	l := strings.ToLower(s)
	for _, x := range []string{"://", "magnet:", "tracker", "token=", "api_key", "password", "authorization", "bearer ", "guid=", "cookie", "\\"} {
		if strings.Contains(l, x) {
			return false
		}
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// sampleFromRecord 先校验当前完整事实，再用冻结title_hash取证，晚来源不得混入。
func sampleFromRecord(ctx context.Context, q proposalQuery, r ComparisonRecord) (LearningSample, error) {
	v := LearningSample{Protocol: 1, Ref: SampleRef{SampleID: r.SampleID, Digest: r.Digest}, JobID: r.JobID, JobVersion: r.JobVersion, PlanVersion: r.PlanVersion, ContextVersion: r.ContextVersion, ParserVersion: r.ParserVersion, Changes: r.Changes, Contexts: []LearningContext{}}
	if r.SchemaVersion != 1 || r.Origin != "human" {
		return v, errors.New("sample_ineligible")
	}
	var body string
	if err := q.QueryRowContext(ctx, "SELECT body FROM jobs WHERE id=?", r.JobID).Scan(&body); err != nil {
		return v, err
	}
	var j Job
	if json.Unmarshal([]byte(body), &j) != nil || j.Plan == nil || j.Plan.Version != r.PlanVersion || PlanDigest(j.Plan) != r.Digest || j.ContextVersion != r.ContextVersion || j.Import != "imported" || j.Discovery != "confirmed" || j.BlockReason != "" || j.ErrorCode != "" || r.ParserVersion != TitleParserVersion || len(r.Changes) == 0 || len(r.Changes) > 32 || len(r.Evidence) == 0 || len(r.Evidence) > 16 {
		return v, errors.New("sample_ineligible")
	}
	var count, linked, videos, found int
	if err := q.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(state='linked'),0),coalesce(sum(video),0),coalesce(sum(video AND discovered),0) FROM file_execution WHERE job_id=? AND plan_version=?`, r.JobID, r.PlanVersion).Scan(&count, &linked, &videos, &found); err != nil {
		return v, err
	}
	if count != len(r.Changes) || linked != count || videos == 0 || found != videos {
		return v, errors.New("sample_ineligible")
	}
	for _, c := range r.Changes {
		if !c.SemanticsResolved || !local(c.Source) || !local(c.Target) || !learningText(c.Source, 512) || !learningText(c.Target, 768) {
			return v, errors.New("sample_ineligible")
		}
	}
	for _, e := range r.Evidence {
		if !subscriptionID.MatchString(e.Subscription) {
			return v, errors.New("sample_ineligible")
		}
		var title string
		if err := q.QueryRowContext(ctx, `SELECT title FROM release_context WHERE job_id=? AND subscription=? AND title_hash=? LIMIT 1`, r.JobID, e.Subscription, e.TitleHash).Scan(&title); err != nil {
			return v, err
		}
		if !learningText(title, 1024) {
			return v, errors.New("sample_ineligible")
		}
		v.Contexts = append(v.Contexts, LearningContext{e.Subscription, title})
	}
	v.Ref.ExportDigest = digestStringJSON(v)
	return v, nil
}

func currentSample(ctx context.Context, q proposalQuery, ref SampleRef) (LearningSample, error) {
	var b string
	var frozen LearningSample
	if err := q.QueryRowContext(ctx, `SELECT body FROM learning_samples WHERE sample_id=?`, ref.SampleID).Scan(&b); err != nil {
		return frozen, errors.New("sample_stale")
	}
	if json.Unmarshal([]byte(b), &frozen) != nil || frozen.Ref != ref {
		return frozen, errors.New("sample_stale")
	}
	var raw string
	var r ComparisonRecord
	if err := q.QueryRowContext(ctx, `SELECT body FROM comparison_records WHERE job_id=? AND plan_version=?`, frozen.JobID, frozen.PlanVersion).Scan(&raw); err != nil {
		return frozen, errors.New("sample_stale")
	}
	if json.Unmarshal([]byte(raw), &r) != nil {
		return frozen, errors.New("sample_stale")
	}
	now, err := sampleFromRecord(ctx, q, r)
	if err != nil || now.Ref != ref {
		return frozen, errors.New("sample_stale")
	}
	return frozen, nil
}

// LearningSamples 原子分配feed序号；只扫描已完整入库记录，读取上限由协议固定。
func (s *Store) LearningSamples(ctx context.Context, after int64) (LearningPage, error) {
	p := LearningPage{Protocol: 1, Items: []LearningEntry{}, Next: after}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.body FROM comparison_records c JOIN jobs j ON j.id=c.job_id WHERE json_extract(j.body,'$.import')='imported' AND json_extract(j.body,'$.discovery')='confirmed' AND c.plan_version=json_extract(j.body,'$.plan.version') AND NOT EXISTS(SELECT 1 FROM learning_scans ls WHERE ls.sample_id=json_extract(c.body,'$.sample_id') AND ls.job_version=j.version) AND NOT EXISTS(SELECT 1 FROM learning_samples l WHERE l.sample_id=json_extract(c.body,'$.sample_id')) ORDER BY c.job_id,c.plan_version LIMIT 100`)
	if err != nil {
		return p, err
	}
	records := []ComparisonRecord{}
	for rows.Next() {
		var b string
		var r ComparisonRecord
		if err = rows.Scan(&b); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			break
		}
		records = append(records, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	for _, r := range records {
		var jobVersion int
		if err = tx.QueryRowContext(ctx, `SELECT version FROM jobs WHERE id=?`, r.JobID).Scan(&jobVersion); err != nil {
			return p, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO learning_scans VALUES(?,?) ON CONFLICT(sample_id) DO UPDATE SET job_version=excluded.job_version`, r.SampleID, jobVersion); err != nil {
			return p, err
		}
		v, e := sampleFromRecord(ctx, tx, r)
		if e != nil {
			if e.Error() == "sample_ineligible" {
				continue
			}
			return p, e
		}
		b, _ := json.Marshal(v)
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO learning_samples(sample_id,body) VALUES(?,?)`, v.Ref.SampleID, string(b)); err != nil {
			return p, err
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT seq,body FROM learning_samples WHERE seq>? ORDER BY seq LIMIT 8`, after)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var b string
		var v LearningEntry
		if err = rows.Scan(&v.Seq, &b); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(b), &v.Sample); err != nil {
			break
		}
		p.Items = append(p.Items, v)
		p.Next = v.Seq
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
