package nas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Engine 单实例串行执行；外部效果可重试，SQLite 保留恢复点。
type Engine struct {
	Store   *Store
	Files   *Files
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
		id, err := e.Store.Ingest(ctx, subscription, r.GUID, r.Hash, r.Magnet, days)
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
			return e.fail(ctx, j, "seed_stop_failed")
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
		if err = e.QB.Add(ctx, j.Magnet); err != nil {
			return e.fail(ctx, &j, "download_add_failed")
		}
		j.Download = "requested"
		if err = e.Store.Save(ctx, &j, "download_submitted"); err != nil {
			return err
		}
	}
	if j.Download != "completed" {
		status, err := e.QB.Status(ctx, j.Hash)
		if err != nil {
			return e.fail(ctx, &j, "download_status_failed")
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
	if j.Import == "pending" {
		sources, err := e.QB.Files(ctx, j.Hash)
		if err != nil {
			return e.fail(ctx, &j, "download_files_failed")
		}
		j.Sources = sources

		var videos []string
		for _, src := range j.Sources {
			ext := strings.ToLower(path.Ext(src))
			if ext == ".mkv" || ext == ".mp4" {
				videos = append(videos, src)
			}
		}
		if len(videos) != 1 {
			j.Import = "needs_confirmation"
			return e.Store.Save(ctx, &j, "needs_confirmation")
		}
		p, err := e.Files.PreviewContext(ctx, videos[0], j.Version+1)
		if err != nil {
			if err.Error() == "needs_confirmation" {
				j.Import = "needs_confirmation"
				return e.Store.Save(ctx, &j, "needs_confirmation")
			}
			return e.fail(ctx, &j, err.Error())
		}
		p.Approved = true
		j.Plan = p
		j.Import = "planned"
		if err = e.Store.Save(ctx, &j, "plan_ready"); err != nil {
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
		// 先验证所有指纹再开始；单个链接与状态提交间崩溃靠同 inode 恢复。
		for _, f := range j.Plan.Files {
			fp, err := e.Files.FingerprintContext(ctx, f.Source)
			if err != nil {
				return e.fail(ctx, &j, err.Error())
			}
			if fp != f.Fingerprint {
				return e.fail(ctx, &j, "fingerprint_changed")
			}
		}
		for _, f := range j.Plan.Files {
			if err = e.Files.LinkContext(ctx, f); err != nil {
				return e.fail(ctx, &j, err.Error())
			}
		}
		j.Import = "imported"
		j.ErrorCode = ""
		if err = e.Store.Save(ctx, &j, "imported"); err != nil {
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
		found, err := e.Emby.Discover(ctx, filepath.Join(e.Library, j.Plan.Files[0].Target))
		if err != nil {
			return e.fail(ctx, &j, "discovery_failed")
		}
		if found {
			j.Discovery = "confirmed"
			j.ErrorCode = ""
			return e.Store.Save(ctx, &j, "discovered")
		}
	}
	return nil
}

// PlanDigest 绑定全部目标、源指纹和方案版本，不包含批准状态。
func PlanDigest(p *Plan) string {
	copy := *p
	copy.Approved = false
	b, _ := json.Marshal(copy)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Propose 只为已下载、未导入的单视频提供人工方案，源必须属于下载器清单。
func (e *Engine) Propose(ctx context.Context, id, source, target string) (*Plan, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if j.Download != "completed" || j.Import == "imported" {
		return nil, errors.New("invalid_job_state")
	}
	found := false
	videos := 0
	for _, s := range j.Sources {
		if ext := strings.ToLower(path.Ext(s)); ext == ".mkv" || ext == ".mp4" {
			videos++
		}
		if s == source {
			found = true
		}
	}
	if !found {
		return nil, errors.New("unknown_source")
	}
	if videos != 1 || (strings.ToLower(path.Ext(source)) != ".mkv" && strings.ToLower(path.Ext(source)) != ".mp4") {
		return nil, errors.New("unsupported_file_set")
	}
	p, err := e.Files.ProposedPlan(source, target, j.Version+1)
	if err != nil {
		return nil, err
	}
	j.Plan = p
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
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if j.Import != "needs_confirmation" || j.Plan == nil || j.Plan.Version != version || PlanDigest(j.Plan) != digest {
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
	j.Import = "planned"
	return e.Store.Save(ctx, &j, "plan_confirmed")
}
