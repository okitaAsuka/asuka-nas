package nas

import (
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const TitleLimit = 8192

type EpisodeRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type TitlePreview struct {
	Normalized    string        `json:"normalized"`
	Group         string        `json:"group,omitempty"`
	Season        int           `json:"season,omitempty"`
	Relative      *EpisodeRange `json:"relative,omitempty"`
	Absolute      *EpisodeRange `json:"absolute,omitempty"`
	Collection    bool          `json:"collection"`
	ParserVersion int           `json:"parser_version"`
}

func normalizeTitle(raw string) (string, error) {
	if len(raw) > TitleLimit || !utf8.ValidString(raw) {
		return "", errors.New("title_invalid")
	}
	s := norm.NFKC.String(html.UnescapeString(raw))
	for _, r := range s {
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return "", errors.New("title_invalid")
		}
	}
	s = strings.NewReplacer("～", "~", "–", "-", "—", "-", "〜", "~").Replace(s)
	return strings.Join(strings.Fields(s), " "), nil
}

var numericGroup = regexp.MustCompile(`^[0-9]+$`)
var bracketEpisode = regexp.MustCompile(`\[([0-9]{1,3})(?:[~\-]([0-9]{1,3}))?\]`)
var dashEpisode = regexp.MustCompile(` - ([0-9]{1,3})(?:[~\-]([0-9]{1,3}))?(?:[\] .]|$)`)
var absoluteEpisode = regexp.MustCompile(`总第\s*([0-9]{1,3})(?:[~\-]([0-9]{1,3}))?`)
var multiEpisode = regexp.MustCompile(`(?i)S[0-9]+E[0-9]+(?:[-~+ ]*E[0-9]+|[-~+][0-9]+)`)
var seasonEpisode = regexp.MustCompile(`(?i)S([0-9]{1,2})E([0-9]{1,3})(?:[^0-9]|$)`)
var titleSeason = regexp.MustCompile(`(?i)(?:第([一二三四五六七八九十0-9]{1,2})季|([0-9]{1,2})(?:st|nd|rd|th)(?:\b|_))`)

func episodeRange(a, b string) (*EpisodeRange, error) {
	start, _ := strconv.Atoi(a)
	end := start
	if b != "" {
		end, _ = strconv.Atoi(b)
	}
	if start < 1 || end < start || end > 999 || start == 480 || start == 720 {
		return nil, errors.New("episode_ambiguous")
	}
	return &EpisodeRange{start, end}, nil
}

// ParseTitle 只提取显式证据；篇章名不能推导季号，范围不能授权合集执行。
func ParseTitle(raw string) (TitlePreview, error) {
	s, err := normalizeTitle(raw)
	p := TitlePreview{Normalized: s, ParserVersion: 1}
	if err != nil {
		return p, err
	}
	if multiEpisode.MatchString(s) {
		return p, errors.New("collection_preview_only")
	}
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i > 1 {
			g := s[1:i]
			if !numericGroup.MatchString(g) {
				p.Group = g
			}
		}
	}
	merge := func(r *EpisodeRange) error {
		if p.Relative != nil && *p.Relative != *r {
			return errors.New("episode_conflict")
		}
		p.Relative = r
		return nil
	}
	for _, rx := range []*regexp.Regexp{bracketEpisode, dashEpisode} {
		for _, m := range rx.FindAllStringSubmatch(s, -1) {
			r, e := episodeRange(m[1], m[2])
			if e != nil {
				return p, e
			}
			if e = merge(r); e != nil {
				return p, e
			}
		}
	}
	for _, m := range seasonEpisode.FindAllStringSubmatch(s, -1) {
		season, _ := strconv.Atoi(m[1])
		if season < 1 {
			return p, errors.New("season_ambiguous")
		}
		r, e := episodeRange(m[2], "")
		if e != nil {
			return p, e
		}
		if p.Season != 0 && p.Season != season {
			return p, errors.New("season_conflict")
		}
		p.Season = season
		if e = merge(r); e != nil {
			return p, e
		}
	}
	for _, m := range titleSeason.FindAllStringSubmatch(s, -1) {
		v := m[1]
		if v == "" {
			v = m[2]
		}
		n, _ := strconv.Atoi(v)
		if n == 0 {
			n = map[string]int{"一": 1, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9, "十": 10}[v]
		}
		if n < 1 || n > 99 {
			return p, errors.New("season_ambiguous")
		}
		if p.Season != 0 && p.Season != n {
			return p, errors.New("season_conflict")
		}
		p.Season = n
	}
	for _, m := range absoluteEpisode.FindAllStringSubmatch(s, -1) {
		r, e := episodeRange(m[1], m[2])
		if e != nil {
			return p, e
		}
		if p.Absolute != nil && *p.Absolute != *r {
			return p, errors.New("episode_conflict")
		}
		p.Absolute = r
	}
	p.Collection = (p.Relative != nil && p.Relative.End > p.Relative.Start) || (p.Absolute != nil && p.Absolute.End > p.Absolute.Start)
	return p, nil
}

type NamingRule struct {
	ID           string   `json:"id"`
	Revision     int      `json:"revision"`
	SeriesID     string   `json:"series_id"`
	Provider     string   `json:"provider"`
	Order        string   `json:"order"`
	Subscription string   `json:"subscription"`
	Group        string   `json:"group,omitempty"`
	Aliases      []string `json:"aliases"`
	Season       int      `json:"season"`
	Directory    string   `json:"directory"`
	DisplayName  string   `json:"display_name"`
}

var opaqueName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func (r NamingRule) Validate() error {
	if !opaqueName.MatchString(r.ID) || !opaqueName.MatchString(r.SeriesID) || !opaqueName.MatchString(r.Order) || !opaqueName.MatchString(r.Subscription) || r.Provider != "manual" || r.Revision < 1 || r.Season < 1 || r.Season > 99 || !local(r.Directory) || !local(r.DisplayName) || filepath.Base(r.DisplayName) != r.DisplayName || len(r.Aliases) < 1 || len(r.Aliases) > 32 {
		return errors.New("rule_invalid")
	}
	for _, s := range append(append([]string{}, r.Aliases...), r.Group, r.DisplayName, r.Directory) {
		v, e := normalizeTitle(s)
		if e != nil || v != s {
			return errors.New("rule_not_normalized")
		}
	}
	for _, a := range r.Aliases {
		if len(a) < 2 || len(a) > 512 {
			return errors.New("rule_invalid")
		}
	}
	return nil
}

// aliasMatched 仅接受分段起始的完整别名；不做模糊子串匹配。
func aliasMatched(p TitlePreview, alias string) bool {
	s := p.Normalized
	if p.Group != "" {
		s = strings.TrimSpace(strings.TrimPrefix(s, "["+p.Group+"]"))
	}
	for _, part := range strings.Split(s, "/") {
		part = strings.TrimLeft(strings.TrimSpace(part), "[")
		if part == alias {
			return true
		}
		if strings.HasPrefix(part, alias) {
			tail := part[len(alias):]
			if strings.HasPrefix(tail, " [") || strings.HasPrefix(tail, " - ") || strings.HasPrefix(tail, "]") {
				return true
			}
		}
	}
	return false
}
func (r NamingRule) matches(subscription string, p TitlePreview) bool {
	if r.Subscription != subscription || (r.Group != "" && r.Group != p.Group) {
		return false
	}
	for _, a := range r.Aliases {
		if aliasMatched(p, a) {
			return true
		}
	}
	return false
}

type NamingDecision struct {
	SeriesID string `json:"series_id"`
	Provider string `json:"provider"`
	Order    string `json:"order"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Target   string `json:"target"`
}

func (r NamingRule) Resolve(title, source string) (NamingDecision, error) {
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
	if p.Group != "" && f.Group != "" && p.Group != f.Group {
		return d, errors.New("title_file_conflict")
	}
	if p.Collection || f.Collection {
		return d, errors.New("collection_preview_only")
	}
	if p.Relative == nil || f.Relative == nil {
		return d, errors.New("episode_unresolved")
	}
	if *p.Relative != *f.Relative {
		return d, errors.New("title_file_conflict")
	}
	if (p.Season != 0 && p.Season != r.Season) || (f.Season != 0 && f.Season != r.Season) {
		return d, errors.New("season_conflict")
	}
	d = NamingDecision{r.SeriesID, "manual", r.Order, r.Season, p.Relative.Start, filepath.Join(r.Directory, fmt.Sprintf("Season %02d", r.Season), fmt.Sprintf("%s - S%02dE%02d%s", r.DisplayName, r.Season, p.Relative.Start, filepath.Ext(source)))}
	return d, nil
}

// MapAbsolute 是显式有界段的纯函数，不查询元数据，也不执行文件操作。
func MapAbsolute(absolute int, segment EpisodeRange, season, relativeStart int) (int, int, error) {
	if segment.Start < 1 || segment.End < segment.Start || segment.End > 999 || absolute < segment.Start || absolute > segment.End || season < 1 || season > 99 || relativeStart < 1 || relativeStart > 999 || relativeStart+segment.End-segment.Start > 999 {
		return 0, 0, errors.New("absolute_mapping_unresolved")
	}
	return season, relativeStart + absolute - segment.Start, nil
}
