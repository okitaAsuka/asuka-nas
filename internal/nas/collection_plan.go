package nas

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
)

type SourceEvidence struct {
	Source      string `json:"source"`
	Fingerprint string `json:"fingerprint"`
}

// manifestGroups 全包裁决；不按文件顺序分配集号，不忽略不支持视频或字幕。
func manifestGroups(sources []string) ([]string, map[string][]string, error) {
	if len(sources) == 0 || len(sources) > 512 {
		return nil, nil, errors.New("invalid_file_manifest")
	}
	seen := map[string]bool{}
	var videos, subtitles []string
	groups := map[string][]string{}
	for _, src := range sources {
		if !local(src) || seen[src] {
			return nil, nil, errors.New("invalid_file_manifest")
		}
		seen[src] = true
		switch strings.ToLower(path.Ext(src)) {
		case ".mkv", ".mp4":
			videos = append(videos, src)
		case ".srt", ".ass", ".ssa", ".vtt", ".sub", ".idx", ".sup":
			subtitles = append(subtitles, src)
		case ".nfo", ".txt", ".jpg", ".jpeg", ".png", ".sfv", ".md5", ".sha1", ".sha256", ".ttf", ".otf":
		case ".avi", ".mov", ".m4v", ".ts", ".m2ts", ".webm", ".mpg", ".mpeg", ".wmv", ".flv", ".vob", ".iso", ".ogm":
			return nil, nil, errors.New("unsupported_video")
		default:
			return nil, nil, errors.New("unsupported_file_set")
		}
	}
	if len(videos) == 0 || len(videos) > 128 {
		return nil, nil, errors.New("multiple_or_missing_videos")
	}
	sort.Strings(videos)
	for _, v := range videos {
		groups[v] = []string{v}
	}
	for _, sub := range subtitles {
		owner := ""
		for _, v := range videos {
			stem := strings.TrimSuffix(path.Base(v), path.Ext(v))
			name := path.Base(sub)
			if path.Dir(v) == path.Dir(sub) && strings.HasPrefix(name, stem) && subtitleSuffix.MatchString(strings.TrimPrefix(name, stem)) {
				if owner != "" {
					return nil, nil, errors.New("subtitle_mapping_ambiguous")
				}
				owner = v
			}
		}
		if owner == "" {
			return nil, nil, errors.New("subtitle_mapping_ambiguous")
		}
		groups[owner] = append(groups[owner], sub)
	}
	return videos, groups, nil
}
func (e *Engine) collectionPlan(ctx context.Context, j *Job) (*Plan, error) {
	videos, groups, err := manifestGroups(j.Sources)
	if err != nil {
		return nil, err
	}
	epoch, err := e.Store.NamingEpoch(ctx)
	if err != nil {
		return nil, err
	}
	j.NamingEpoch = epoch
	j.CheckedContext = j.ContextVersion
	p := &Plan{Version: j.Version + 1, ParserVersion: 2, RuleEpoch: epoch, ContextVersion: j.ContextVersion, Collection: true, Decisions: map[string]NamingDecision{}}
	episodes := map[int]bool{}
	targets := map[string]bool{}
	var first *NamingDecision
	for _, video := range videos {
		d, refs, err := e.Store.ResolveNaming(ctx, j.ID, video)
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, errors.New("multiple_or_missing_videos")
		}
		if d.ExpectedStart < 1 || d.ExpectedEnd <= d.ExpectedStart {
			return nil, errors.New("collection_mapping_required")
		}
		if first == nil {
			first = d
		} else if first.SeriesID != d.SeriesID || first.Order != d.Order || first.Season != d.Season || first.ExpectedStart != d.ExpectedStart || first.ExpectedEnd != d.ExpectedEnd {
			return nil, errors.New("mapping_conflict")
		}
		if episodes[d.Episode] {
			return nil, errors.New("duplicate_episode")
		}
		episodes[d.Episode] = true
		one, err := e.Files.ProposedManifest(ctx, video, d.Target, p.Version, groups[video])
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
		p.Decisions[video] = *d
		p.RuleRefs = append(p.RuleRefs, refs...)
	}
	if len(episodes) != first.ExpectedEnd-first.ExpectedStart+1 {
		return nil, errors.New("collection_incomplete")
	}
	for i := first.ExpectedStart; i <= first.ExpectedEnd; i++ {
		if !episodes[i] {
			return nil, errors.New("collection_incomplete")
		}
	}
	// 辅助文件虽然不入库，也必须真实存在、不是符号链接并绑定源指纹。
	ordered := append([]string(nil), j.Sources...)
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
			return nil, err
		}
		p.Manifest = append(p.Manifest, SourceEvidence{source, fp})
	}
	if err = e.Store.snapshotEvidence(ctx, j.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}
