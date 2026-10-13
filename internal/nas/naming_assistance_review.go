package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

func namingTexts(explanation string, uncertain []string) bool {
	if !learningText(explanation, 2000) || len(uncertain) > 8 {
		return false
	}
	for _, s := range uncertain {
		if !learningText(s, 512) {
			return false
		}
	}
	return true
}

// suggestionBatch 仅接受完整有限DTO；季号/名称由明确人工事实或当前证据约束，目标用本地算法重算。
func suggestionBatch(v NamingReview, private []namingBinding, r NamingResult) (BatchOrganization, error) {
	b := BatchOrganization{Directory: r.Directory, DisplayName: r.DisplayName, Mode: "manual", Rows: []BatchRow{}}
	if r.Protocol != 1 || r.ID != v.ID || r.Digest != v.Snapshot.Digest || len(r.Files) != len(private) || !namingTexts(r.Explanation, r.Uncertainties) {
		return b, errors.New("naming_invalid")
	}
	if r.Status != "suggestion" && r.Status != "unresolved" {
		return b, errors.New("naming_invalid")
	}
	seen := map[string]NamingSuggestionRow{}
	sources := map[string]string{}
	for _, f := range private {
		sources[f.ID] = f.Source
	}
	for _, row := range r.Files {
		if sources[row.FileID] == "" || seen[row.FileID].FileID != "" || !namingTexts(row.Basis, row.Uncertainties) {
			return b, errors.New("naming_invalid")
		}
		if row.Target != "" && (!local(row.Target) || !learningText(row.Target, 768)) {
			return b, errors.New("naming_invalid")
		}
		seen[row.FileID] = row
	}
	if r.Status == "unresolved" {
		if r.Directory != "" || r.DisplayName != "" {
			return b, errors.New("naming_invalid")
		}
		for _, row := range r.Files {
			if row.Target != "" || row.Season != 0 || row.Episode != 0 {
				return b, errors.New("naming_invalid")
			}
		}
		return b, nil
	}
	if r.Directory == "" || r.DisplayName == "" || r.Directory != v.Snapshot.Hints.Directory || r.DisplayName != v.Snapshot.Hints.DisplayName {
		return b, errors.New("naming_unresolved")
	}
	season := v.Snapshot.Hints.Season
	for _, f := range v.Snapshot.Files {
		if f.Season != 0 {
			if season != 0 && season != f.Season {
				return b, errors.New("season_conflict")
			}
			season = f.Season
		}
	}
	for _, c := range v.Snapshot.Contexts {
		t, err := ParseTitle(c.Title)
		if err != nil {
			return b, errors.New("naming_unresolved")
		}
		if t.Season != 0 {
			if season != 0 && season != t.Season {
				return b, errors.New("season_conflict")
			}
			season = t.Season
		}
	}
	if season < 1 || season > 99 {
		return b, errors.New("naming_unresolved")
	}
	b.Season = season
	ordered := []string{}
	for _, binding := range private {
		ordered = append(ordered, binding.Source)
	}
	videos, groups, err := manifestGroups(ordered)
	if err != nil {
		return b, err
	}
	expectations := map[string]NamingSuggestionRow{}
	for _, video := range videos {
		row := seen[digestString(video)]
		if row.Season != season || row.Episode < 1 || row.Episode > 999 {
			return b, errors.New("naming_invalid")
		}
		target := path.Join(b.Directory, fmt.Sprintf("Season %02d", season), fmt.Sprintf("%s - S%02dE%02d%s", b.DisplayName, season, row.Episode, path.Ext(video)))
		if row.Target != target {
			return b, errors.New("naming_invalid")
		}
		b.Rows = append(b.Rows, BatchRow{video, row.Episode})
		expectations[video] = row
		stem := strings.TrimSuffix(path.Base(video), path.Ext(video))
		targetStem := strings.TrimSuffix(target, path.Ext(target))
		for _, sub := range groups[video][1:] {
			expectations[sub] = NamingSuggestionRow{Season: season, Episode: row.Episode, Target: targetStem + strings.TrimPrefix(path.Base(sub), stem)}
		}
	}
	for _, binding := range private {
		row := seen[binding.ID]
		expected := expectations[binding.Source]
		if row.Season != expected.Season || row.Episode != expected.Episode || row.Target != expected.Target {
			return b, errors.New("naming_invalid")
		}
	}
	return b, nil
}

// SubmitNaming 先处理精确重放，再校验当前快照；不可变结果落盘和事件均先于网络ACK。
func (e *Engine) SubmitNaming(ctx context.Context, r NamingResult) (NamingReview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, private, err := e.Store.namingReview(ctx, r.ID)
	if err != nil {
		return v, err
	}
	resultDigest := digestStringJSON(r)
	if v.Result != nil {
		if v.ResultDigest != resultDigest {
			return v, errors.New("naming_replay_conflict")
		}
		return v, nil
	}
	if v.State != "requested" {
		return v, errors.New("naming_stale")
	}
	if err = e.currentNaming(ctx, v, private); err != nil {
		return v, err
	}
	if _, err = suggestionBatch(v, private, r); err != nil {
		return v, err
	}
	body, _ := json.Marshal(r)
	if len(body) > 32768 {
		return v, errors.New("naming_limit")
	}
	state := "suggested"
	if r.Status == "unresolved" {
		state = "unresolved"
	}
	tx, err := e.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err = namingCurrentTx(ctx, tx, v.Snapshot); err != nil {
		return v, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE naming_requests SET result=?,result_digest=?,state=?,version=version+1 WHERE id=? AND version=? AND state='requested' AND result=''`, string(body), resultDigest, state, v.ID, v.Version)
	if err != nil {
		return v, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return v, errors.New("naming_stale")
	}
	if err = namingAudit(ctx, tx, v.ID, v.Snapshot.JobID, "naming_"+state); err != nil {
		return v, err
	}
	if err = tx.Commit(); err != nil {
		return v, err
	}
	v.State = state
	v.Version++
	v.Result = &r
	v.ResultDigest = resultDigest
	return v, nil
}

// PreviewNaming 所有文件校验复用batch；保存最新成功审核意图，旧preview不能批准新意图。
func (e *Engine) PreviewNaming(ctx context.Context, id string, version int, override *BatchOrganization) (*BatchPreview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, private, err := e.Store.namingReview(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.Version != version || v.State != "suggested" && v.State != "unresolved" {
		return nil, errors.New("naming_stale")
	}
	if err = e.currentNaming(ctx, v, private); err != nil {
		return nil, err
	}
	var b BatchOrganization
	if override != nil {
		b = *override
	} else {
		if v.Result == nil || v.Result.Status != "suggestion" {
			return nil, errors.New("naming_unresolved")
		}
		b, err = suggestionBatch(v, private, *v.Result)
		if err != nil {
			return nil, err
		}
	}
	preview, err := e.batchPreview(ctx, v.Snapshot.JobID, v.Snapshot.JobVersion, b)
	if err != nil {
		return nil, err
	}
	preview.Plan.Assistance = &AssistanceRef{v.ID, v.Snapshot.Digest, v.ResultDigest}
	preview.Digest = PlanDigest(preview.Plan)
	body, _ := json.Marshal(preview)
	tx, err := e.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = namingCurrentTx(ctx, tx, v.Snapshot); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE naming_requests SET preview=? WHERE id=? AND version=? AND state IN ('suggested','unresolved')`, string(body), id, version)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, errors.New("naming_stale")
	}
	if err = namingAudit(ctx, tx, v.ID, v.Snapshot.JobID, "naming_previewed"); err != nil {
		return nil, err
	}
	return preview, tx.Commit()
}

// ConfirmNaming 只确认已持久当前preview；Save事务共同写批准/预约/人工对比/事件，不另建执行器。
func (e *Engine) ConfirmNaming(ctx context.Context, id string, version int, digest string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, private, err := e.Store.namingReview(ctx, id)
	if err != nil {
		return err
	}
	if v.State == "approved" && v.Preview != nil && v.Preview.Digest == digest && v.Version == version+1 {
		return nil
	}
	if v.Version != version || v.State != "suggested" && v.State != "unresolved" {
		return errors.New("naming_stale")
	}
	if v.Preview == nil || v.Preview.Digest != digest || v.Preview.Plan.Batch == nil {
		return errors.New("naming_preview_required")
	}
	if err = e.currentNaming(ctx, v, private); err != nil {
		return err
	}
	j, err := e.Store.Get(ctx, v.Snapshot.JobID)
	if err != nil {
		return err
	}
	p, err := e.batchPlan(ctx, j, *v.Preview.Plan.Batch)
	if err != nil {
		return err
	}
	p.Assistance = &AssistanceRef{v.ID, v.Snapshot.Digest, v.ResultDigest}
	if PlanDigest(p) != digest {
		return errors.New("stale_plan")
	}
	p.Approved = true
	j.Plan = p
	j.Import = "planned"
	j.ErrorCode = ""
	j.BlockReason = ""
	j.CheckedContext = j.ContextVersion
	j.NamingEpoch = p.RuleEpoch
	j.NextAttempt = 0
	return e.Store.Save(ctx, &j, "batch_confirmed")
}

// RejectNaming 保留全部模型依据，只终止此项；新建议必须人工显式申请新快照。
func (e *Engine) RejectNaming(ctx context.Context, id string, version int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, _, err := e.Store.namingReview(ctx, id)
	if err != nil {
		return err
	}
	if v.State == "rejected" && v.Version == version+1 {
		return nil
	}
	if v.Version != version || v.State == "approved" || v.State == "rejected" {
		return errors.New("naming_stale")
	}
	tx, err := e.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE naming_requests SET state='rejected',version=version+1,preview='' WHERE id=? AND version=?`, id, version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("naming_stale")
	}
	if err = namingAudit(ctx, tx, id, v.Snapshot.JobID, "naming_rejected"); err != nil {
		return err
	}
	return tx.Commit()
}

// bindAssistanceTx 标记普通人工入口的模型暴露；专用批准还原子校验已保存preview及结果引用。
func bindAssistanceTx(ctx context.Context, tx *sql.Tx, j *Job, kind string) error {
	p := j.Plan
	if p.Assistance != nil && kind == "batch_confirmed" {
		var state, snapshot, resultDigest, preview string
		if err := tx.QueryRowContext(ctx, `SELECT state,snapshot,result_digest,preview FROM naming_requests WHERE id=? AND job_id=?`, p.Assistance.ID, j.ID).Scan(&state, &snapshot, &resultDigest, &preview); err != nil {
			return err
		}
		var snap NamingSnapshot
		var saved BatchPreview
		if json.Unmarshal([]byte(snapshot), &snap) != nil || json.Unmarshal([]byte(preview), &saved) != nil || state != "suggested" && state != "unresolved" || p.Assistance.SnapshotDigest != snap.Digest || p.Assistance.ResultDigest != resultDigest || saved.Digest != PlanDigest(p) {
			return errors.New("naming_preview_required")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE naming_requests SET state='approved',version=version+1 WHERE id=?`, p.Assistance.ID); err != nil {
			return err
		}
		return namingAudit(ctx, tx, p.Assistance.ID, j.ID, "naming_approved")
	}
	// legacy单文件入口同样标记模型接触；此引用无需读取未授权文件或修改既有比较。
	var id, snapshot, resultDigest string
	err := tx.QueryRowContext(ctx, `SELECT id,snapshot,result_digest FROM naming_requests WHERE job_id=? AND result!='' ORDER BY seq DESC LIMIT 1`, j.ID).Scan(&id, &snapshot, &resultDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var snap NamingSnapshot
	if json.Unmarshal([]byte(snapshot), &snap) != nil {
		return errors.New("naming_invalid")
	}
	if p.Assistance == nil {
		return errors.New("naming_preview_required")
	}
	return nil
}

// namingCurrentTx 防止RSS/规则写入在外部指纹校验与结果/preview提交之间改变逻辑快照。
func namingCurrentTx(ctx context.Context, tx *sql.Tx, s NamingSnapshot) error {
	var version int
	var epoch int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM jobs WHERE id=?`, s.JobID).Scan(&version); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT value FROM naming_epoch WHERE id=1`).Scan(&epoch); err != nil {
		return err
	}
	if version != s.JobVersion || epoch != s.RuleEpoch {
		return errors.New("naming_stale")
	}
	return nil
}
