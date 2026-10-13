package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"
	"time"
)

// ComparisonRecord 是不可变的人工作业意图；不包含GUID、秘密URL或凭据。
// 执行事实另外按(job,plan,index)读取，部分链接不是成功样本。
type ComparisonRecord struct {
	SampleID       string             `json:"sample_id"`
	SchemaVersion  int                `json:"schema_version"`
	Origin         string             `json:"origin"`
	JobID          string             `json:"job_id"`
	JobVersion     int                `json:"job_version"`
	ContextVersion int64              `json:"context_version"`
	PlanVersion    int                `json:"plan_version"`
	Digest         string             `json:"digest"`
	ParserVersion  int                `json:"parser_version"`
	CreatedAt      int64              `json:"created_at"`
	Organization   *BatchOrganization `json:"organization,omitempty"`
	Manifest       []SourceEvidence   `json:"manifest"`
	Evidence       []ContextEvidence  `json:"evidence"`
	Changes        []ComparisonChange `json:"changes"`
}
type ComparisonChange struct {
	SemanticsResolved bool     `json:"semantics_resolved"`
	Source            string   `json:"source"`
	Target            string   `json:"target"`
	Season            int      `json:"season"`
	Episode           int      `json:"episode"`
	BeforeSeason      int      `json:"before_season,omitempty"`
	BeforeEpisode     int      `json:"before_episode,omitempty"`
	BeforeAbsolute    int      `json:"before_absolute,omitempty"`
	ChangedFields     []string `json:"changed_fields"`
}
type ComparisonView struct {
	Intent    ComparisonRecord `json:"intent"`
	Files     []FileExecution  `json:"files"`
	Status    string           `json:"status"`
	ErrorCode string           `json:"error_code,omitempty"`
}

func digestStringJSON(v any) string { b, _ := json.Marshal(v); return digestString(string(b)) }

func recordComparisonTx(ctx context.Context, tx *sql.Tx, j *Job) error {
	p := j.Plan
	if p != nil && len(p.Evidence) > 100 {
		return errors.New("comparison_limit")
	}
	if p == nil || !p.Approved {
		return errors.New("invalid_plan")
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM comparison_records").Scan(&count); err != nil {
		return err
	}
	if count >= 10000 {
		return errors.New("comparison_limit")
	}
	r := ComparisonRecord{SampleID: digestString(j.ID + ":" + strconv.Itoa(p.Version) + ":" + PlanDigest(p)), SchemaVersion: 1, Origin: "human", JobID: j.ID, JobVersion: j.Version, ContextVersion: p.ContextVersion, PlanVersion: p.Version, Digest: PlanDigest(p), ParserVersion: p.ParserVersion, CreatedAt: time.Now().Unix(), Organization: p.Batch, Manifest: p.Manifest, Evidence: p.Evidence}
	for _, f := range p.Files {
		originalVideo := f.Source
		semantics := targetSemantics(f.Target)
		if !videoTarget(f.Target) { // 字幕沿用主视频的明确季集。
			for _, v := range p.Files {
				if videoTarget(v.Target) && subtitleOwner(v.Source, f.Source) {
					semantics = targetSemantics(v.Target)
					originalVideo = v.Source
					break
				}
			}
		}
		resolved := semantics != nil
		if !resolved {
			// 保留旧单视频opaque目标能力；明确标未知，不能假造学习季集。
			if p.Batch != nil {
				return errors.New("manual_semantics_unresolved")
			}
			semantics = &ManualSemantics{}
		}
		c := ComparisonChange{SemanticsResolved: resolved, Source: f.Source, Target: f.Target, Season: semantics.Season, Episode: semantics.Episode, ChangedFields: []string{"target_relative_name"}}
		if videoTarget(originalVideo) {
			t, err := ParseTitle(strings.TrimSuffix(path.Base(originalVideo), path.Ext(originalVideo)))
			if err == nil {
				c.BeforeSeason = t.Season
				if t.Relative != nil && t.Relative.Start == t.Relative.End {
					c.BeforeEpisode = t.Relative.Start
				}
				if t.Absolute != nil && t.Absolute.Start == t.Absolute.End {
					c.BeforeAbsolute = t.Absolute.Start
				}
			}
		}
		if path.Dir(f.Source) != path.Dir(f.Target) {
			c.ChangedFields = append(c.ChangedFields, "directory")
		}
		if before := targetSemantics(originalVideo); before != nil && resolved && before.Identity != semantics.Identity {
			c.ChangedFields = append(c.ChangedFields, "display_name")
		}
		if resolved && c.BeforeSeason != c.Season {
			c.ChangedFields = append(c.ChangedFields, "season")
		}
		if resolved && c.BeforeEpisode != c.Episode {
			c.ChangedFields = append(c.ChangedFields, "episode")
		}
		if originalVideo != f.Source {
			c.ChangedFields = append(c.ChangedFields, "inherited_video_mapping")
		}
		r.Changes = append(r.Changes, c)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return errors.New("comparison_limit")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO comparison_records(job_id,plan_version,body) VALUES(?,?,?)", j.ID, p.Version, string(b))
	return err
}

// ComparisonPage 只由本机管理API调用；每页20份，历史永不静默裁剪。
func (s *Store) ComparisonPage(ctx context.Context, id string, after int) ([]ComparisonView, int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT body FROM comparison_records WHERE job_id=? AND plan_version>? ORDER BY plan_version LIMIT 21", id, after)
	if err != nil {
		return nil, 0, err
	}
	intents := []ComparisonRecord{}
	for rows.Next() {
		var b string
		var r ComparisonRecord
		if err = rows.Scan(&b); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(b), &r); err != nil {
			break
		}
		intents = append(intents, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	next := 0
	if len(intents) > 20 {
		intents = intents[:20]
		next = intents[19].PlanVersion
	}
	j, err := s.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	views := []ComparisonView{}
	for _, r := range intents {
		files, err := s.FileExecutions(ctx, id, r.PlanVersion)
		if err != nil {
			return nil, 0, err
		}
		v := ComparisonView{Intent: r, Files: files, Status: "confirmed_intent"}
		linked, found, videos := 0, 0, 0
		for _, f := range files {
			if f.State == "linked" {
				linked++
			}
			if f.Video {
				videos++
				if f.Discovered {
					found++
				}
			}
		}
		if linked > 0 {
			v.Status = "partial"
		}
		if len(files) == len(r.Changes) && linked == len(r.Changes) {
			v.Status = "awaiting_discovery"
		}
		// 最终领域状态提交成功前不声称整包完成。
		if j.Plan == nil || j.Plan.Version != r.PlanVersion {
			v.Status = "superseded"
		} else {
			v.ErrorCode = j.ErrorCode
			if len(files) == len(r.Changes) && linked == len(r.Changes) && videos > 0 && found == videos && j.Import == "imported" && j.Discovery == "confirmed" && j.BlockReason == "" {
				v.Status = "complete"
			}
		}
		views = append(views, v)
	}
	return views, next, nil
}

func subtitleOwner(video, sub string) bool {
	stem := strings.TrimSuffix(path.Base(video), path.Ext(video))
	return path.Dir(video) == path.Dir(sub) && strings.HasPrefix(path.Base(sub), stem) && subtitleSuffix.MatchString(strings.TrimPrefix(path.Base(sub), stem))
}

// 只取有界引用，不把GUID/原始RSS标题复制到人工记录；证据版本在Plan中冻结。
func (s *Store) snapshotBatchEvidence(ctx context.Context, id string, p *Plan) error {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT subscription,title_hash FROM release_context WHERE job_id=? ORDER BY subscription,title_hash LIMIT 101", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c ContextEvidence
		if err = rows.Scan(&c.Subscription, &c.TitleHash); err != nil {
			return err
		}
		p.Evidence = append(p.Evidence, c)
		if len(p.Evidence) > 100 {
			return errors.New("comparison_limit")
		}
	}
	return rows.Err()
}
