package nas

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// BatchOrganization 只描述本次完整任务的人工意图，不是可启用规则。
type BatchOrganization struct {
	Directory      string     `json:"directory"`
	DisplayName    string     `json:"display_name"`
	Season         int        `json:"season"`
	Mode           string     `json:"mode"` // manual / extracted / sequential
	Offset         int        `json:"offset"`
	Start          int        `json:"start"`
	OrderConfirmed bool       `json:"order_confirmed"`
	Rows           []BatchRow `json:"rows"`
}
type BatchRow struct {
	Source  string `json:"source"`
	Episode int    `json:"episode"`
}
type BatchPreview struct {
	JobVersion int    `json:"job_version"`
	Plan       *Plan  `json:"plan"`
	Digest     string `json:"digest"`
}

func (e *Engine) BatchPreview(ctx context.Context, id string, version int, b BatchOrganization) (*BatchPreview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.batchPreview(ctx, id, version, b)
}
func (e *Engine) batchPreview(ctx context.Context, id string, version int, b BatchOrganization) (*BatchPreview, error) {
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if j.Version != version {
		return nil, errors.New("stale_version")
	}
	p, err := e.batchPlan(ctx, j, b)
	if err != nil {
		return nil, err
	}
	return &BatchPreview{j.Version, p, PlanDigest(p)}, nil
}

// batchPlan 全包独立验证；行序只在显式确认连续编号时有意义。
func (e *Engine) batchPlan(ctx context.Context, j Job, b BatchOrganization) (*Plan, error) {
	if j.Plan != nil && j.Plan.Started {
		return nil, errors.New("plan_frozen")
	}
	if j.Download != "completed" || j.Import == "imported" || j.BlockReason == "legacy_evidence_unresolved" || j.BlockReason == "late_mapping_conflict" {
		return nil, errors.New("invalid_job_state")
	}
	videos, groups, err := manifestGroups(j.Sources)
	if err != nil {
		return nil, err
	}
	if !local(b.Directory) || !local(b.DisplayName) || path.Base(b.DisplayName) != b.DisplayName || len(b.Directory) > 512 || len(b.DisplayName) > 200 || b.Season < 1 || b.Season > 99 {
		return nil, errors.New("invalid_batch")
	}
	for _, value := range []string{b.Directory, b.DisplayName} {
		normalized, err := normalizeTitle(value)
		if err != nil || value != normalized {
			return nil, errors.New("invalid_batch")
		}
	}
	if len(b.Rows) != len(videos) {
		return nil, errors.New("collection_incomplete")
	}
	if b.Offset < -998 || b.Offset > 998 {
		return nil, errors.New("episode_out_of_bounds")
	}
	switch b.Mode {
	case "manual":
		if b.Offset != 0 || b.Start != 0 || b.OrderConfirmed {
			return nil, errors.New("invalid_batch")
		}
	case "extracted":
		if b.Start != 0 || b.OrderConfirmed {
			return nil, errors.New("invalid_batch")
		}
	case "sequential":
		if !b.OrderConfirmed {
			return nil, errors.New("order_confirmation_required")
		}
		if b.Offset != 0 || b.Start < 1 || b.Start+len(videos)-1 > 999 {
			return nil, errors.New("episode_out_of_bounds")
		}
	default:
		return nil, errors.New("invalid_batch")
	}
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return nil, err
	}
	p := &Plan{Version: j.Version + 1, ParserVersion: TitleParserVersion, RuleEpoch: epoch, ContextVersion: j.ContextVersion, Collection: len(videos) > 1, Batch: &b}
	owners := map[string]bool{}
	for _, v := range videos {
		owners[v] = true
	}
	seen, episodes, targets := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for i, r := range b.Rows {
		if !owners[r.Source] || seen[r.Source] {
			return nil, errors.New("invalid_file_manifest")
		}
		seen[r.Source] = true
		t, err := ParseTitle(strings.TrimSuffix(path.Base(r.Source), path.Ext(r.Source)))
		if err != nil {
			return nil, err
		}
		if t.Collection {
			return nil, errors.New("collection_preview_only")
		}
		if t.Season != 0 && t.Season != b.Season {
			return nil, errors.New("season_conflict")
		}
		if b.Mode == "extracted" {
			number := t.Relative
			if number == nil {
				number = t.Absolute
			}
			// 双编号需要明确选择对应规则，不能在本次偏移中偷选其一。
			if number == nil || t.Relative != nil && t.Absolute != nil {
				return nil, errors.New("episode_unresolved")
			}
			if r.Episode != number.Start+b.Offset {
				return nil, errors.New("invalid_batch")
			}
		}
		if b.Mode == "sequential" && r.Episode != b.Start+i {
			return nil, errors.New("invalid_batch")
		}
		if r.Episode < 1 || r.Episode > 999 {
			return nil, errors.New("episode_out_of_bounds")
		}
		if episodes[r.Episode] {
			return nil, errors.New("duplicate_episode")
		}
		episodes[r.Episode] = true
		target := path.Join(b.Directory, fmt.Sprintf("Season %02d", b.Season), fmt.Sprintf("%s - S%02dE%02d%s", b.DisplayName, b.Season, r.Episode, path.Ext(r.Source)))
		if targetSemantics(target) == nil {
			return nil, errors.New("invalid_batch")
		}
		// 复用已验证的单组文件层，但分组必须先经过完整 manifestGroups 门禁。
		one, err := e.Files.ProposedManifest(ctx, r.Source, target, p.Version, groups[r.Source])
		if err != nil {
			return nil, err
		}
		for _, f := range one.Files {
			if targets[f.Target] {
				return nil, errors.New("target_conflict")
			}
			targets[f.Target] = true
			p.Files = append(p.Files, f)
		}
	}
	if err = e.bindManifest(ctx, j.Sources, p); err != nil {
		return nil, err
	}
	if err = e.Store.snapshotEvidence(ctx, j.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}

// bindManifest 包括不入库的辅助文件；confirm/execute均核对完整清单指纹。
func (e *Engine) bindManifest(ctx context.Context, sources []string, p *Plan) error {
	ordered := append([]string(nil), sources...)
	sort.Strings(ordered)
	fingerprints := map[string]string{}
	for _, f := range p.Files {
		fingerprints[f.Source] = f.Fingerprint
	}
	for _, source := range ordered {
		fp := fingerprints[source]
		var err error
		if fp == "" {
			fp, err = e.Files.FingerprintContext(ctx, source)
		}
		if err != nil {
			return err
		}
		p.Manifest = append(p.Manifest, SourceEvidence{source, fp})
	}
	return nil
}
func (e *Engine) BatchConfirm(ctx context.Context, id string, version, planVersion int, digest string, b BatchOrganization) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.batchConfirm(ctx, id, version, planVersion, digest, b)
}
func (e *Engine) batchConfirm(ctx context.Context, id string, version, planVersion int, digest string, b BatchOrganization) error {
	j, err := e.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	// 网络重试/双击只接受已持久的同一意图，不因执行推进再次造样本。
	if j.Plan != nil && j.Plan.Batch != nil && j.Plan.Approved && j.Plan.Version == planVersion && PlanDigest(j.Plan) == digest {
		if digestStringJSON(b) != digestStringJSON(*j.Plan.Batch) {
			return errors.New("stale_plan")
		}
		return nil
	}
	if j.Version != version {
		return errors.New("stale_version")
	}
	p, err := e.batchPlan(ctx, j, b)
	if err != nil {
		return err
	}
	if p.Version != planVersion || PlanDigest(p) != digest {
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
