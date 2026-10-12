package nas

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var numberToken = regexp.MustCompile(`(?i)( - |总第\s*|S[0-9]+E|\[)([0-9][^\s\[\]]*)`)
var integerToken = regexp.MustCompile(`^[0-9]{1,3}(?:[~-][0-9]{1,3})?$`)
var qualityToken = regexp.MustCompile(`(?i)^[0-9]{3,4}[pi]$`)
var yearToken = regexp.MustCompile(`^[12][0-9]{3}$`)

// 完整消费数字 token，再让证据解析器提取；禁止只取范围/小数的合法前缀。
func validateEpisodeTokens(s string) error {
	for _, m := range numberToken.FindAllStringSubmatch(s, -1) {
		token := m[2]
		if strings.HasPrefix(strings.ToUpper(m[1]), "S") {
			season, e := strconv.Atoi(m[1][1 : len(m[1])-1])
			if e != nil || season < 1 || season > 99 {
				return errors.New("season_ambiguous")
			}
		}
		if m[1] == "[" && (qualityToken.MatchString(token) || yearToken.MatchString(token)) {
			continue
		}
		for _, ext := range []string{".mkv", ".mp4"} {
			if strings.HasSuffix(strings.ToLower(token), ext) {
				token = token[:len(token)-len(ext)]
				break
			}
		}
		if !integerToken.MatchString(token) {
			return errors.New("unsupported_episode_kind")
		}
	}
	return nil
}

// 所有 manifest 项使用同一特殊篇门禁，不能通过旁车或辅助文件隐藏证据。
func releaseKind(raw string) error {
	s, e := normalizeTitle(raw)
	if e != nil {
		return e
	}
	if hasSpecialEpisode(s) {
		return errors.New("unsupported_episode_kind")
	}
	return nil
}
