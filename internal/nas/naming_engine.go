package nas

import (
	"context"
	"errors"
)

// namingCheck 先检查来源变化；晚到矛盾只报告，不改动历史目标。
func (e *Engine) namingCheck(ctx context.Context, j *Job) error {
	if j.BlockReason == "legacy_evidence_unresolved" {
		return nil
	}
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return err
	}
	frozen := j.Import == "imported" || (j.Plan != nil && j.Plan.Started)
	if frozen {
		if j.CheckedContext == j.ContextVersion && j.BlockReason != "late_mapping_conflict" {
			return nil
		}
		code := ""
		if err := e.Store.checkFrozenEvidence(ctx, j); err != nil {
			code = "late_mapping_conflict"
			if err.Error() == "legacy_evidence_unresolved" {
				code = err.Error()
			}
		}

		if code == j.BlockReason && j.CheckedContext == j.ContextVersion {
			return nil
		}
		j.CheckedContext = j.ContextVersion
		if code != "" {
			j.BlockReason = code
			j.ErrorCode = code
		}
		kind := "source_context_checked"
		if code == "" && j.BlockReason == "late_mapping_conflict" {
			j.BlockReason = ""
			if j.ErrorCode == "late_mapping_conflict" {
				j.ErrorCode = ""
			}
			kind = "mapping_conflict_cleared"
		}
		if code != "" {
			kind = code
		}
		return e.Store.Save(ctx, j, kind)
	}
	if j.Plan != nil && (j.Plan.RuleEpoch != epoch || j.Plan.ContextVersion != j.ContextVersion) {
		if j.BlockReason == "stale_plan" {
			return nil
		}
		j.Import = "needs_confirmation"
		j.BlockReason = "stale_plan"
		j.ErrorCode = "stale_plan"
		return e.Store.Save(ctx, j, "plan_stale")
	}
	if j.Import == "needs_confirmation" && j.Plan == nil && (j.NamingEpoch != epoch || j.CheckedContext != j.ContextVersion) {
		j.Import = "pending"
		j.BlockReason = ""
		j.ErrorCode = ""
		j.NextAttempt = 0
		return e.Store.Save(ctx, j, "naming_retry_ready")
	}
	return nil
}
func (e *Engine) namingPlan(ctx context.Context, j *Job, video string) (*Plan, error) {
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return nil, err
	}
	j.NamingEpoch = epoch
	j.CheckedContext = j.ContextVersion
	d, refs, err := e.Store.ResolveNaming(ctx, j.ID, video)
	if err != nil {
		return nil, err
	}
	var p *Plan
	if d != nil {
		if d.ExpectedEnd > d.ExpectedStart {
			return nil, errors.New("collection_incomplete")
		}
		p, err = e.Files.ProposedManifest(ctx, video, d.Target, j.Version+1, j.Sources)
	} else {
		p, err = e.Files.PreviewManifest(ctx, video, j.Version+1, j.Sources)
	}
	if err != nil {
		return nil, err
	}
	p.ParserVersion = TitleParserVersion
	p.RuleEpoch = epoch
	p.ContextVersion = j.ContextVersion
	p.Decision = d
	p.RuleRefs = refs
	if d == nil {
		p.Manual = targetSemantics(p.Files[0].Target)
	}
	if err = e.Store.snapshotEvidence(ctx, j.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}
func (e *Engine) bindManualPlan(ctx context.Context, j Job, p *Plan) error {
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return err
	}
	if j.Plan != nil && j.Plan.Started {
		return errors.New("plan_frozen")
	}
	d, refs, err := e.Store.ResolveNaming(ctx, j.ID, p.Files[0].Source)
	if err != nil {
		return err
	}
	if d != nil && d.Target != p.Files[0].Target {
		return errors.New("mapping_conflict")
	}
	p.ParserVersion = TitleParserVersion
	p.RuleEpoch = epoch
	p.ContextVersion = j.ContextVersion
	p.Decision = d
	p.RuleRefs = refs
	if d == nil {
		p.Manual = targetSemantics(p.Files[0].Target)
	}
	if err = e.Store.snapshotEvidence(ctx, j.ID, p); err != nil {
		return err
	}
	return nil
}

func namingBlock(code string) bool {
	switch code {
	case "rule_normalization_unresolved", "rule_scope_ambiguous", "collection_mapping_required", "collection_incomplete", "duplicate_episode", "absolute_relative_conflict", "absolute_mapping_unresolved", "episode_out_of_bounds", "unsupported_episode_kind", "invalid_file_manifest", "unsupported_video", "unsupported_file_set", "multiple_or_missing_videos", "needs_confirmation", "subtitle_mapping_ambiguous", "title_invalid", "episode_ambiguous", "episode_conflict", "season_conflict", "season_ambiguous", "collection_preview_only", "episode_unresolved", "title_file_conflict", "rule_disabled", "mapping_conflict", "source_mapping_unresolved":
		return true
	}
	return false
}

func namingFailure(code string) string {
	switch code {
	case "unsafe_path", "unsafe_or_missing_path", "unsafe_target", "target_conflict", "cross_device", "file_changed", "file_read_failed", "not_regular_file", "operation_cancelled", "invalid_target_extension", "unknown_source":
		return code
	}
	return "naming_io_failed"
}
