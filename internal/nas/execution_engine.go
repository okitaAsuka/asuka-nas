package nas

import (
	"context"
	"errors"
	"path/filepath"
)

// FileOperations 保留小的文件边界，允许 fake 权限故障验证恢复，不引入调度框架。
type FileOperations interface {
	FingerprintContext(context.Context, string) (string, error)
	PreviewManifest(context.Context, string, int, []string) (*Plan, error)
	ProposedManifest(context.Context, string, string, int, []string) (*Plan, error)
	CheckTarget(PlannedFile) error
	LinkContext(context.Context, PlannedFile) error
}

func (e *Engine) executePlan(ctx context.Context, j *Job) error {
	// 每轮先完整预检；半包失败不改变已成功硬链，也不假报 imported。
	checked := map[string]string{}
	for _, source := range j.Plan.Manifest {
		fp, err := e.Files.FingerprintContext(ctx, source.Source)
		if err != nil {
			return e.fail(ctx, j, err.Error())
		}
		checked[source.Source] = fp
		if fp != source.Fingerprint {
			return e.fail(ctx, j, "fingerprint_changed")
		}
	}
	for _, f := range j.Plan.Files {
		fp := checked[f.Source]
		var err error
		if fp == "" {
			fp, err = e.Files.FingerprintContext(ctx, f.Source)
		}
		if err != nil {
			return e.fail(ctx, j, err.Error())
		}
		if fp != f.Fingerprint {
			return e.fail(ctx, j, "fingerprint_changed")
		}
		if err = e.Files.CheckTarget(f); err != nil {
			return e.fail(ctx, j, err.Error())
		}
	}
	if !j.Plan.Started {
		j.Plan.Started = true
		if err := e.Store.Save(ctx, j, "import_started"); err != nil {
			return err
		}
	}
	records, err := e.Store.FileExecutions(ctx, j.ID, j.Plan.Version)
	if err != nil {
		return err
	}
	if len(records) != len(j.Plan.Files) {
		return errors.New("execution_records_missing")
	}
	for i, record := range records {
		if record.Index != i {
			return errors.New("execution_records_invalid")
		}
		if record.State == "pending" {
			if err = e.Store.MarkFile(ctx, j, i, "file_intent"); err != nil {
				return err
			}
		}
		// linked 也用安全幂等链接重新核实；外部删除不应变成虚假的成功状态。
		if err = e.Files.LinkContext(ctx, j.Plan.Files[i]); err != nil {
			return e.fail(ctx, j, err.Error())
		}
		if record.State != "linked" {
			if err = e.Store.MarkFile(ctx, j, i, "file_linked"); err != nil {
				return err
			}
		}
	}
	j.Import = "imported"
	j.ErrorCode = ""
	return e.Store.Save(ctx, j, "imported")
}
func (e *Engine) discoverPlan(ctx context.Context, j *Job) error {
	records, err := e.Store.FileExecutions(ctx, j.ID, j.Plan.Version)
	if err != nil {
		return err
	}
	if len(records) != len(j.Plan.Files) {
		return errors.New("execution_records_missing")
	}
	all := true
	videos := 0
	for _, record := range records {
		if record.Index < 0 || record.Index >= len(j.Plan.Files) || record.State != "linked" {
			return errors.New("execution_records_invalid")
		}
		if !record.Video {
			continue
		}
		videos++
		if record.Discovered {
			continue
		}
		found, err := e.Emby.Discover(ctx, filepath.Join(e.Library, j.Plan.Files[record.Index].Target))
		if err != nil {
			return e.fail(ctx, j, "discovery_failed")
		}
		if found {
			if err = e.Store.MarkFile(ctx, j, record.Index, "video_discovered"); err != nil {
				return err
			}
		} else {
			all = false
		}
	}
	if videos == 0 {
		return errors.New("plan_missing_video")
	}
	if all {
		j.Discovery = "confirmed"
		j.ErrorCode = ""
		return e.Store.Save(ctx, j, "discovered")
	}
	return nil
}
