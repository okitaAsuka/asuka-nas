package nas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

// Engine 单实例串行执行；外部效果可重试，SQLite 保留恢复点。
type Engine struct {
	Store   *Store
	Files   FileOperations
	QB      Downloader
	Emby    Media
	Library string
	mu      sync.Mutex
}

// IngestRSS 不联网；调用方提供受限 RSS reader。
func (e *Engine) IngestRSS(ctx context.Context, subscription string, r io.Reader, days int) ([]string, error) {
	resources, err := ParseRSS(r)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range resources {
		id, err := e.Store.IngestRelease(ctx, subscription, r.GUID, r.Hash, r.Magnet, nil, days, r.Title)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Tick 即使某个 job 失败也处理其他 job；做种检查先于任何文件操作。
func (e *Engine) Tick(ctx context.Context, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ids, err := e.Store.IDs(ctx)
	if err != nil {
		return err
	}
	var failures []error
	failed := map[string]bool{}
	for _, id := range ids {
		j, err := e.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if err = e.stop(ctx, &j, now); err != nil {
			failures = append(failures, err)
			failed[id] = true
		}
	}
	for _, id := range ids {
		if failed[id] {
			continue
		}
		if err = e.tick(ctx, id, now); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (e *Engine) fail(ctx context.Context, j *Job, code string) error {
	if j.ErrorCode != code {
		j.ErrorCode = code
		if err := e.Store.Save(ctx, j, "operation_failed"); err != nil {
			return err
		}
	}
	return errors.New(code)
}

func (e *Engine) stop(ctx context.Context, j *Job, now time.Time) error {
	if j.CompletedAt > 0 && now.Unix() >= j.StopAt && j.Seed != "stopped" {
		if err := e.QB.Stop(ctx, j.Hash); err != nil {
			return e.fail(ctx, j, qbFailureCode(err, "seed_stop_failed"))
		}
		j.Seed = "stopped"
		return e.Store.Save(ctx, j, "seed_stopped")
	}
	return nil
}

func (e *Engine) tick(ctx context.Context, id string, now time.Time) error {
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	// 恢复后先依据持久完成时间停止，qB 文件列表故障也不能阻塞计时。
	if err = e.stop(ctx, &j, now); err != nil {
		return err
	}
	if j.Download == "pending" {
		j.Download = "requesting"
		if err = e.Store.Save(ctx, &j, "download_requested"); err != nil {
			return err
		}
	}
	if j.Download == "requesting" {
		if j.HasTorrent {
			payload, loadErr := e.Store.Torrent(ctx, j.Hash)
			if loadErr != nil {
				return e.fail(ctx, &j, "torrent_payload_unavailable")
			}
			hash, checkErr := TorrentHash(payload)
			if checkErr != nil || hash != j.Hash {
				return e.fail(ctx, &j, "torrent_payload_invalid")
			}
			err = e.QB.AddTorrent(ctx, payload)
		} else {
			err = e.QB.Add(ctx, j.Magnet)
		}
		if err != nil {
			return e.fail(ctx, &j, qbFailureCode(err, "download_add_failed"))
		}
		j.Download = "requested"
		if err = e.Store.Save(ctx, &j, "download_submitted"); err != nil {
			return err
		}
	}
	if j.Download != "completed" {
		status, err := e.QB.Status(ctx, j.Hash)
		if err != nil {
			return e.fail(ctx, &j, qbFailureCode(err, "download_status_failed"))
		}
		if !status.Complete {
			return nil
		}
		if status.CompletedAt <= 0 || status.CompletedAt > now.Unix() {
			return e.fail(ctx, &j, "invalid_completion_time")
		}
		j.CompletedAt = status.CompletedAt
		j.StopAt = status.CompletedAt + int64(j.SeedDays)*86400
		j.Seed = "seeding"
		j.Download = "completed"

		if err = e.Store.Save(ctx, &j, "download_completed"); err != nil {
			return err
		}
		if err = e.stop(ctx, &j, now); err != nil {
			return err
		}
	}
	if err = e.namingCheck(ctx, &j); err != nil {
		return err
	}
	if j.BlockReason == "late_mapping_conflict" || j.BlockReason == "legacy_evidence_unresolved" {
		return nil
	}
	if j.Import == "pending" {
		sources, err := e.QB.Files(ctx, j.Hash)
		if err != nil {
			return e.fail(ctx, &j, qbFailureCode(err, "download_files_failed"))
		}
		j.Sources = sources

		j.NamingEpoch, err = e.Store.NamingEpoch(ctx)
		if err != nil {
			return err
		}
		j.CheckedContext = j.ContextVersion
		video, reason := singleVideo(j.Sources)
		var p *Plan
		if reason == "multiple_or_missing_videos" {
			p, err = e.collectionPlan(ctx, &j)
		} else if reason != "" {
			err = errors.New(reason)
		} else {
			p, err = e.namingPlan(ctx, &j, video)
		}
		if err != nil {
			if !namingBlock(err.Error()) {
				return e.fail(ctx, &j, namingFailure(err.Error()))
			}
			j.Import = "needs_confirmation"
			j.ErrorCode = err.Error()
			j.BlockReason = err.Error()
			return e.Store.Save(ctx, &j, "needs_confirmation")
		}
		p.Approved = true
		j.Plan = p
		j.Import = "planned"
		if err = e.Store.Save(ctx, &j, "plan_ready"); err != nil {
			if err.Error() == "target_reserved" {
				j.Plan = nil
				j.Import = "needs_confirmation"
				j.BlockReason = "target_reserved"
				j.ErrorCode = "target_reserved"
				return e.Store.Save(ctx, &j, "needs_confirmation")
			}
			return err
		}
	}
	if j.Import == "needs_confirmation" {
		return nil
	}
	if j.Import == "planned" {
		if j.Plan == nil || !j.Plan.Approved {
			return nil
		}

		if err = e.executePlan(ctx, &j); err != nil {
			return err
		}
	}
	if j.Import == "imported" && j.Refresh == "pending" {
		if err = e.Emby.Refresh(ctx); err != nil {
			return e.fail(ctx, &j, "refresh_failed")
		}
		j.Refresh = "requested"
		j.ErrorCode = ""
		if err = e.Store.Save(ctx, &j, "refresh_requested"); err != nil {
			return err
		}
	}
	if j.Refresh == "requested" && j.Discovery != "confirmed" {
		return e.discoverPlan(ctx, &j)
	}
	return nil
}

// PlanDigest 绑定全部目标、源指纹和方案版本，不包含批准状态。
func PlanDigest(p *Plan) string {
	copy := *p
	copy.Approved = false
	copy.Started = false
	b, _ := json.Marshal(copy)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Propose 只为已下载、未导入的单视频提供人工方案，源必须属于下载器清单。
func (e *Engine) Propose(ctx context.Context, id, source, target string) (*Plan, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.propose(ctx, id, source, target)
}
func (e *Engine) propose(ctx context.Context, id, source, target string) (*Plan, error) {
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if j.Download != "completed" || j.Import == "imported" {
		return nil, errors.New("invalid_job_state")
	}
	video, reason := singleVideo(j.Sources)
	if reason != "" {
		return nil, errors.New(reason)
	}
	if video != source {
		return nil, errors.New("unknown_source")
	}
	p, err := e.Files.ProposedManifest(ctx, source, target, j.Version+1, j.Sources)
	if err != nil {
		return nil, err
	}
	if err = e.bindManualPlan(ctx, j, p); err != nil {
		return nil, err
	}
	j.Plan = p
	j.BlockReason = "awaiting_plan_confirmation"
	j.Import = "needs_confirmation"
	if err = e.Store.Save(ctx, &j, "plan_ready"); err != nil {
		return nil, err
	}
	return p, nil
}

// Confirm 版本和摘要双重绑定，文件变化或目标冲突由后续执行再次校验。
func (e *Engine) Confirm(ctx context.Context, id string, version int, digest string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.confirm(ctx, id, version, digest)
}
func (e *Engine) confirm(ctx context.Context, id string, version int, digest string) error {
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if j.Import != "needs_confirmation" || j.Plan == nil || j.Plan.Version != version || PlanDigest(j.Plan) != digest {
		return errors.New("stale_plan")
	}
	if err = e.namingCheck(ctx, &j); err != nil {
		return err
	}
	if j.BlockReason == "stale_plan" {
		return errors.New("stale_plan")
	}
	for _, f := range j.Plan.Files {
		fp, err := e.Files.FingerprintContext(ctx, f.Source)
		if err != nil {
			return err
		}
		if fp != f.Fingerprint {
			return errors.New("fingerprint_changed")
		}
	}
	for _, f := range j.Plan.Files {
		if err = e.Files.CheckTarget(f); err != nil {
			return err
		}
	}
	j.Plan.Approved = true
	j.BlockReason = ""
	j.ErrorCode = ""
	j.Import = "planned"
	return e.Store.Save(ctx, &j, "plan_confirmed")
}
