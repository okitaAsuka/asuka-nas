package nas

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// AbsoluteMapping 只覆盖用户明确确认的有限段，不允许无限 offset 外推。
type AbsoluteMapping struct {
	Start         int `json:"start"`
	End           int `json:"end"`
	RelativeStart int `json:"relative_start"`
}

func (r NamingRule) bounds() *EpisodeRange {
	if r.Episodes != nil {
		return r.Episodes
	}
	if r.Absolute != nil {
		return &EpisodeRange{r.Absolute.RelativeStart, r.Absolute.RelativeStart + r.Absolute.End - r.Absolute.Start}
	}
	return nil
}
func (r NamingRule) absoluteRange(a *EpisodeRange) (*EpisodeRange, error) {
	if a == nil {
		return nil, nil
	}
	if r.Absolute == nil {
		return nil, errors.New("absolute_mapping_unresolved")
	}
	_, start, e := MapAbsolute(a.Start, EpisodeRange{r.Absolute.Start, r.Absolute.End}, r.Season, r.Absolute.RelativeStart)
	if e != nil {
		return nil, e
	}
	_, end, e := MapAbsolute(a.End, EpisodeRange{r.Absolute.Start, r.Absolute.End}, r.Season, r.Absolute.RelativeStart)
	if e != nil {
		return nil, e
	}
	return &EpisodeRange{start, end}, nil
}
func (r NamingRule) resolvedRange(p TitlePreview) (*EpisodeRange, error) {
	absolute, e := r.absoluteRange(p.Absolute)
	if e != nil {
		return nil, e
	}
	relative := p.Relative
	if relative != nil && absolute != nil && *relative != *absolute {
		return nil, errors.New("absolute_relative_conflict")
	}
	if relative == nil {
		relative = absolute
	}
	if relative == nil {
		return nil, errors.New("episode_unresolved")
	}
	if b := r.bounds(); b != nil && (relative.Start < b.Start || relative.End > b.End) {
		return nil, errors.New("episode_out_of_bounds")
	}
	return relative, nil
}

// resolveMapped 每个文件必须自带唯一编号；标题范围只做验证，不充当序号分配器。
func (r NamingRule) resolveMapped(title, source string) (NamingDecision, error) {
	var d NamingDecision
	p, e := ParseTitle(title)
	if e != nil {
		return d, e
	}
	if !r.matches(r.Subscription, p) {
		return d, errors.New("rule_not_matched")
	}
	f, e := ParseTitle(strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)))
	if e != nil {
		return d, e
	}
	if p.Season != 0 && p.Season != r.Season || f.Season != 0 && f.Season != r.Season {
		return d, errors.New("season_conflict")
	}
	if p.Group != "" && f.Group != "" && p.Group != f.Group {
		return d, errors.New("title_file_conflict")
	}
	if specialEpisode.MatchString(f.Normalized) || f.Collection {
		return d, errors.New("unsupported_episode_kind")
	}
	expected, e := r.resolvedRange(p)
	if e != nil {
		return d, e
	}
	// 无季号且只有裸整数，只有落在相对界外、绝对界内时才可唯一解释为总集数。
	// 相对界内始终优先相对；显式总第证据仍必须交叉校验，绝不忽略矛盾。
	if f.Absolute == nil && f.Season == 0 && f.Relative != nil && r.Absolute != nil {
		b := r.bounds()
		if b != nil && (f.Relative.Start < b.Start || f.Relative.End > b.End) {
			f.Absolute = f.Relative
			f.Relative = nil
		}
	}
	episode, e := r.resolvedRange(f)
	if e != nil {
		return d, e
	}
	if episode.Start != episode.End {
		return d, errors.New("unsupported_episode_kind")
	}
	if episode.Start < expected.Start || episode.End > expected.End {
		return d, errors.New("title_file_conflict")
	}
	d = NamingDecision{expected.Start, expected.End, r.SeriesID, "manual", r.Order, r.Season, episode.Start, filepath.Join(r.Directory, fmt.Sprintf("Season %02d", r.Season), fmt.Sprintf("%s - S%02dE%02d%s", r.DisplayName, r.Season, episode.Start, filepath.Ext(source)))}
	return d, nil
}
