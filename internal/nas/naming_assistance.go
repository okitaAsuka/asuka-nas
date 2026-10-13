package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// 新扩展仅迁移此未部署项目；快照、结果和审计不可改写，审核状态与文件方案原子提交。
const assistanceSchema = `
CREATE TABLE IF NOT EXISTS naming_requests(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,job_id TEXT NOT NULL REFERENCES jobs(id),version INTEGER NOT NULL,state TEXT NOT NULL,snapshot TEXT NOT NULL,bindings TEXT NOT NULL,result TEXT NOT NULL DEFAULT '',result_digest TEXT NOT NULL DEFAULT '',preview TEXT NOT NULL DEFAULT '');
CREATE TRIGGER IF NOT EXISTS naming_snapshot_immutable BEFORE UPDATE OF snapshot,bindings ON naming_requests BEGIN SELECT RAISE(ABORT,'immutable_snapshot'); END;
CREATE TRIGGER IF NOT EXISTS naming_result_immutable BEFORE UPDATE OF result,result_digest ON naming_requests WHEN OLD.result!='' BEGIN SELECT RAISE(ABORT,'immutable_result'); END;
CREATE TRIGGER IF NOT EXISTS naming_no_delete BEFORE DELETE ON naming_requests BEGIN SELECT RAISE(ABORT,'immutable_request'); END;
CREATE TABLE IF NOT EXISTS naming_assistance_audit(seq INTEGER PRIMARY KEY AUTOINCREMENT,request_id TEXT NOT NULL,kind TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS naming_audit_no_update BEFORE UPDATE ON naming_assistance_audit BEGIN SELECT RAISE(ABORT,'immutable_audit'); END;
CREATE TRIGGER IF NOT EXISTS naming_audit_no_delete BEFORE DELETE ON naming_assistance_audit BEGIN SELECT RAISE(ABORT,'immutable_audit'); END;
`

// NamingHints 是人工明确事实；缺少季号时模型不能填默认季号。
type NamingHints struct {
	Directory   string `json:"directory"`
	DisplayName string `json:"display_name"`
	Season      int    `json:"season"`
}

// NamingFile 隐去下载目录，用不透明ID映射NAS私有完整清单。
type NamingFile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
}

// NamingSnapshot 同时绑定文件内容/身份、任务版本、上下文和命名规则epoch。
type NamingSnapshot struct {
	Protocol       int               `json:"protocol"`
	ID             string            `json:"id"`
	Digest         string            `json:"digest"`
	JobID          string            `json:"job_id"`
	JobVersion     int               `json:"job_version"`
	ContextVersion int64             `json:"context_version"`
	ParserVersion  int               `json:"parser_version"`
	RuleEpoch      int64             `json:"rule_epoch"`
	ManifestDigest string            `json:"manifest_digest"`
	Hints          NamingHints       `json:"hints"`
	Contexts       []LearningContext `json:"contexts"`
	Files          []NamingFile      `json:"files"`
}

// NamingSuggestionRow 每文件给出有限季集、目标和依据；辅助文件无目标。
type NamingSuggestionRow struct {
	FileID        string   `json:"file_id"`
	Season        int      `json:"season"`
	Episode       int      `json:"episode"`
	Target        string   `json:"target"`
	Basis         string   `json:"basis"`
	Uncertainties []string `json:"uncertainties"`
}

// NamingResult 无脚本/工具/规则字段，完整未解结果也持久化，不自动付费重试。
type NamingResult struct {
	Protocol      int                   `json:"protocol"`
	ID            string                `json:"id"`
	Digest        string                `json:"digest"`
	Status        string                `json:"status"`
	Directory     string                `json:"directory"`
	DisplayName   string                `json:"display_name"`
	Files         []NamingSuggestionRow `json:"files"`
	Explanation   string                `json:"explanation"`
	Uncertainties []string              `json:"uncertainties"`
}

// AssistanceRef 永久保留模型来源；人工修订/拒绝不把它转换为原始人工证据。
type AssistanceRef struct {
	ID             string `json:"id"`
	SnapshotDigest string `json:"snapshot_digest"`
	ResultDigest   string `json:"result_digest"`
}

// NamingReview 是只在本机审核面可读的状态，不用于普通通知正文。
type NamingReview struct {
	ID           string         `json:"id"`
	Version      int            `json:"version"`
	State        string         `json:"state"`
	Snapshot     NamingSnapshot `json:"snapshot"`
	Result       *NamingResult  `json:"result,omitempty"`
	ResultDigest string         `json:"result_digest"`
	Preview      *BatchPreview  `json:"preview,omitempty"`
}
type namingBinding struct{ ID, Source, Fingerprint string }

func namingAudit(ctx context.Context, tx *sql.Tx, id, job, kind string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO naming_assistance_audit(request_id,kind,created_at) VALUES(?,?,?)`, id, kind, time.Now().Unix()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO events(job_id,kind,created_at) VALUES(?,?,?)`, job, kind, time.Now().Unix())
	return err
}
func (s *Store) namingReview(ctx context.Context, id string) (NamingReview, []namingBinding, error) {
	var v NamingReview
	var snapshot, bindings, result, preview string
	err := s.db.QueryRowContext(ctx, `SELECT id,version,state,snapshot,bindings,result,result_digest,preview FROM naming_requests WHERE id=?`, id).Scan(&v.ID, &v.Version, &v.State, &snapshot, &bindings, &result, &v.ResultDigest, &preview)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil, errors.New("naming_not_found")
	}
	if err != nil {
		return v, nil, err
	}
	var private []namingBinding
	if json.Unmarshal([]byte(snapshot), &v.Snapshot) != nil || json.Unmarshal([]byte(bindings), &private) != nil {
		return v, nil, errors.New("naming_invalid")
	}
	if result != "" {
		v.Result = &NamingResult{}
		if json.Unmarshal([]byte(result), v.Result) != nil {
			return v, nil, errors.New("naming_invalid")
		}
	}
	if preview != "" {
		v.Preview = &BatchPreview{}
		if json.Unmarshal([]byte(preview), v.Preview) != nil {
			return v, nil, errors.New("naming_invalid")
		}
	}
	return v, private, nil
}

// RequestNaming 先规则和全清单门禁，再导出一份显式授权快照；没有模型或远端访问。
func (e *Engine) RequestNaming(ctx context.Context, jobID string, version int, hints NamingHints, retry bool) (NamingReview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var empty NamingReview
	j, err := e.Store.Get(ctx, jobID)
	if err != nil {
		return empty, err
	}
	if j.Version != version {
		return empty, errors.New("stale_version")
	}
	if j.Download != "completed" || j.Import != "needs_confirmation" || j.Plan != nil && (j.Plan.Started || j.Plan.Approved) {
		return empty, errors.New("invalid_job_state")
	}
	videos, groups, err := manifestGroups(j.Sources)
	if err != nil {
		return empty, err
	}
	if len(j.Sources) > 32 {
		return empty, errors.New("naming_limit")
	}
	for _, video := range videos {
		d, _, resolveErr := e.Store.ResolveNaming(ctx, j.ID, video)
		if resolveErr == nil && d != nil {
			return empty, errors.New("naming_rules_available")
		}
	}
	if hints.Season < 0 || hints.Season > 99 || hints.Directory != "" && (!local(hints.Directory) || !learningText(hints.Directory, 512)) || hints.DisplayName != "" && (!local(hints.DisplayName) || path.Base(hints.DisplayName) != hints.DisplayName || !learningText(hints.DisplayName, 200)) {
		return empty, errors.New("naming_invalid")
	}
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return empty, err
	}
	snapshot := NamingSnapshot{Protocol: 1, JobID: j.ID, JobVersion: j.Version, ContextVersion: j.ContextVersion, ParserVersion: TitleParserVersion, RuleEpoch: epoch, Hints: hints, Contexts: []LearningContext{}, Files: []NamingFile{}}
	contexts, err := e.Store.Contexts(ctx, j.ID)
	if err != nil {
		return empty, err
	}
	if len(contexts) > 16 {
		return empty, errors.New("naming_limit")
	}
	for _, c := range contexts {
		if !subscriptionID.MatchString(c.Subscription) || !learningText(c.Title, 1024) || absoluteContextPath.MatchString(c.Title) {
			return empty, errors.New("naming_invalid")
		}
		snapshot.Contexts = append(snapshot.Contexts, LearningContext{c.Subscription, c.Title})
	}
	ordered := append([]string(nil), j.Sources...)
	sort.Strings(ordered)
	private := []namingBinding{}
	for _, source := range ordered {
		name := path.Base(source)
		if !learningText(name, 512) {
			return empty, errors.New("naming_invalid")
		}
		fp, err := e.Files.FingerprintContext(ctx, source)
		if err != nil {
			return empty, err
		}
		id := digestString(source)
		private = append(private, namingBinding{id, source, fp})
		f := NamingFile{ID: id, Name: name, Kind: "auxiliary"}
		if videoTarget(source) {
			f.Kind = "video"
			p, err := ParseTitle(strings.TrimSuffix(name, path.Ext(name)))
			if err != nil {
				return empty, err
			}
			if p.Collection {
				return empty, errors.New("collection_preview_only")
			}
			f.Season = p.Season
			if p.Relative != nil && p.Relative.Start == p.Relative.End {
				f.Episode = p.Relative.Start
			}
		}
		for _, g := range groups {
			for _, sub := range g[1:] {
				if sub == source {
					f.Kind = "subtitle"
				}
			}
		}
		snapshot.Files = append(snapshot.Files, f)
	}
	snapshot.ManifestDigest = digestStringJSON(private)
	rows, err := e.Store.db.QueryContext(ctx, `SELECT id FROM naming_requests WHERE job_id=? ORDER BY seq DESC`, j.ID)
	if err != nil {
		return empty, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return empty, err
	}
	if len(ids) > 0 {
		prior, bindings, err := e.Store.namingReview(ctx, ids[0])
		if err != nil {
			return empty, err
		}
		same := prior.Snapshot
		same.ID = ""
		same.Digest = ""
		if !retry && digestStringJSON(same) == digestStringJSON(snapshot) {
			return prior, nil
		}
		if !retry {
			return empty, errors.New("naming_retry_required")
		}
		if prior.State != "rejected" && prior.State != "unresolved" && e.currentNaming(ctx, prior, bindings) == nil {
			return empty, errors.New("naming_retry_required")
		}
	}
	if len(ids) >= 3 {
		return empty, errors.New("naming_limit")
	}
	snapshot.ID = digestString(j.ID + ":" + fmt.Sprint(len(ids)+1) + ":" + digestStringJSON(snapshot))
	snapshot.Digest = digestStringJSON(snapshot)
	body, _ := json.Marshal(snapshot)
	if len(body) > 32768 {
		return empty, errors.New("naming_limit")
	}
	bindingBody, _ := json.Marshal(private)
	tx, err := e.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	// 与其它后端写入同样做CAS；版本和epoch变化不能在快照授权后滑入。
	var current int
	var ruleEpoch int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM jobs WHERE id=?`, j.ID).Scan(&current); err != nil {
		return empty, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT value FROM naming_epoch WHERE id=1`).Scan(&ruleEpoch); err != nil {
		return empty, err
	}
	if current != j.Version || ruleEpoch != epoch {
		return empty, errors.New("naming_stale")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO naming_requests(id,job_id,version,state,snapshot,bindings) VALUES(?,?,1,'requested',?,?)`, snapshot.ID, j.ID, string(body), string(bindingBody)); err != nil {
		return empty, err
	}
	if err = namingAudit(ctx, tx, snapshot.ID, j.ID, "naming_requested"); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return NamingReview{ID: snapshot.ID, Version: 1, State: "requested", Snapshot: snapshot}, nil
}

// currentNaming 在每次模型调用前/结果接收/预览/批准都校验当前任务和完整私有指纹。
func (e *Engine) currentNaming(ctx context.Context, v NamingReview, private []namingBinding) error {
	j, err := e.Store.Get(ctx, v.Snapshot.JobID)
	if err != nil {
		return errors.New("naming_stale")
	}
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return err
	}
	if j.Version != v.Snapshot.JobVersion || j.ContextVersion != v.Snapshot.ContextVersion || epoch != v.Snapshot.RuleEpoch || TitleParserVersion != v.Snapshot.ParserVersion || j.Download != "completed" || j.Import != "needs_confirmation" || j.Plan != nil && (j.Plan.Approved || j.Plan.Started) || len(j.Sources) != len(private) {
		return errors.New("naming_stale")
	}
	sources := append([]string(nil), j.Sources...)
	sort.Strings(sources)
	for i, b := range private {
		if sources[i] != b.Source {
			return errors.New("naming_stale")
		}
		fp, err := e.Files.FingerprintContext(ctx, b.Source)
		if err != nil || fp != b.Fingerprint {
			return errors.New("naming_stale")
		}
	}
	return nil
}

// NamingReviews 只读当前审核视图；历史快照/结果不会被动态stale标签改写。
func (e *Engine) NamingReviews(ctx context.Context, job string) ([]NamingReview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rows, err := e.Store.db.QueryContext(ctx, `SELECT id FROM naming_requests WHERE job_id=? ORDER BY seq LIMIT 3`, job)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []NamingReview{}
	for _, id := range ids {
		v, b, err := e.Store.namingReview(ctx, id)
		if err != nil {
			return nil, err
		}
		if v.State != "approved" && v.State != "rejected" && e.currentNaming(ctx, v, b) != nil {
			v.State = "stale"
		}
		items = append(items, v)
	}
	return items, nil
}

func (s *Store) assistanceRef(ctx context.Context, job string) (*AssistanceRef, error) {
	var id, snapshot, resultDigest string
	err := s.db.QueryRowContext(ctx, `SELECT id,snapshot,result_digest FROM naming_requests WHERE job_id=? AND result!='' ORDER BY seq DESC LIMIT 1`, job).Scan(&id, &snapshot, &resultDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v NamingSnapshot
	if json.Unmarshal([]byte(snapshot), &v) != nil {
		return nil, errors.New("naming_invalid")
	}
	return &AssistanceRef{id, v.Digest, resultDigest}, nil
}

// 目录只在NAS私有binding出现；拒绝上下文中独立绝对路径或盘符引用。
var absoluteContextPath = regexp.MustCompile(`(^|[\s"\'=])(/[^\s]+|[A-Za-z]:/)`)
