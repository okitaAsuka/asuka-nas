package nas

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// TickJobs 先运行既有下载/做种/文件循环，再按显式opt-in创建受限模型快照；离线不等待agent。
func (d *Daemon) TickJobs(ctx context.Context, now time.Time) error {
	runErr := d.Engine.RetryTick(ctx, now, d.Config.TickSeconds)
	if !d.Config.Learning.AutomaticNaming {
		return runErr
	}
	if !d.Config.Learning.Enabled {
		return errors.Join(runErr, errors.New("naming_automatic_service_required"))
	}
	return errors.Join(runErr, d.Engine.queueAutomaticNaming(ctx))
}

// queueAutomaticNaming 每tick最多8份；同逻辑命名事实已有任何请求即跳过，不轮询大文件内容。
// 下载文件保持只读是既有前提；外部改写仍在check/submit/preview校验并只接受人工显式retry。
func (e *Engine) queueAutomaticNaming(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return err
	}
	var cursor string
	if err = e.Store.db.QueryRowContext(ctx, `SELECT cursor FROM naming_auto_scan WHERE id=1`).Scan(&cursor); err != nil {
		return err
	}
	query := `SELECT j.body FROM jobs j
 WHERE j.id>? AND json_extract(j.body,'$.download')='completed' AND json_extract(j.body,'$.import')='needs_confirmation'
 AND json_extract(j.body,'$.plan') IS NULL
 AND NOT EXISTS(SELECT 1 FROM naming_requests terminal WHERE terminal.job_id=j.id AND terminal.state IN ('rejected','unresolved') AND terminal.seq=(SELECT max(last.seq) FROM naming_requests last WHERE last.job_id=j.id))
 AND json_extract(j.body,'$.block_reason') IN ('needs_confirmation','multiple_or_missing_videos','episode_unresolved','source_mapping_unresolved','absolute_mapping_unresolved','season_ambiguous','season_conflict','title_file_conflict','episode_conflict','episode_ambiguous','mapping_conflict','rule_scope_ambiguous','rule_normalization_unresolved','collection_mapping_required','collection_incomplete','duplicate_episode','absolute_relative_conflict','episode_out_of_bounds')
 AND NOT EXISTS(SELECT 1 FROM naming_requests n WHERE n.job_id=j.id
  AND json_extract(n.snapshot,'$.context_version')=json_extract(j.body,'$.context_version')
  AND json_extract(n.snapshot,'$.parser_version')=? AND json_extract(n.snapshot,'$.rule_epoch')=?
  AND coalesce(json_extract(n.snapshot,'$.plan_digest'),'')=''
  AND json_array_length(n.bindings)=json_array_length(j.body,'$.sources')
  AND NOT EXISTS(SELECT 1 FROM json_each(j.body,'$.sources') src WHERE NOT EXISTS(SELECT 1 FROM json_each(n.bindings) b WHERE json_extract(b.value,'$.Source')=src.value)))
 ORDER BY j.id LIMIT 8`
	readPage := func(after string) ([]Job, error) {
		rows, err := e.Store.db.QueryContext(ctx, query, after, TitleParserVersion, epoch)
		if err != nil {
			return nil, err
		}
		jobs := []Job{}
		for rows.Next() {
			var body string
			var j Job
			if err = rows.Scan(&body); err != nil {
				break
			}
			if err = json.Unmarshal([]byte(body), &j); err != nil {
				break
			}
			jobs = append(jobs, j)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		return jobs, err
	}
	jobs, err := readPage(cursor)
	if err != nil {
		return err
	}
	if len(jobs) == 0 && cursor != "" {
		jobs, err = readPage("")
		if err != nil {
			return err
		}
	}
	next := ""
	if len(jobs) > 0 {
		next = jobs[len(jobs)-1].ID
	}
	// 预约扫描游标，坏输入或总预算已满不能永久堵住后面的支持任务。
	if _, err = e.Store.db.ExecContext(ctx, `UPDATE naming_auto_scan SET cursor=? WHERE id=1 AND cursor!=?`, next, next); err != nil {
		return err
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 新命名语义可自动介入；最新拒绝/未解需人工恢复，无hints不能补造身份/季号。
		_, err = e.requestNaming(ctx, j.ID, j.Version, NamingHints{}, true, "automatic")
		if err != nil {
			switch err.Error() {
			case "naming_limit", "naming_rules_available", "naming_stale", "naming_invalid", "naming_retry_required", "invalid_job_state", "stale_version", "unsupported_episode_kind", "unsupported_video", "unsupported_file_set", "invalid_file_manifest", "subtitle_mapping_ambiguous", "collection_preview_only", "multiple_or_missing_videos", "title_invalid", "episode_ambiguous", "season_ambiguous":
				continue
			}
			return errors.New("naming_automatic_unavailable")
		}
	}
	return nil
}
