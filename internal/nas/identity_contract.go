package nas

import (
	"errors"
	"strings"
	"unicode"
)

// identityNormalizationVersion=0 是历史未标版本；1 为当前比较视图。
// 只规范化比较用的 alias/group 副本，绝不更新不可变规则、目录或计划摘要。
const identityNormalizationVersion = 1

func (r NamingRule) comparisonView() (NamingRule, error) {
	if r.IdentityNormalizationVersion < 0 || r.IdentityNormalizationVersion > identityNormalizationVersion {
		return r, errors.New("rule_normalization_unresolved")
	}
	aliases := make([]string, len(r.Aliases))
	for i, a := range r.Aliases {
		v, e := normalizeTitle(a)
		if e != nil || v == "" {
			return r, errors.New("rule_normalization_unresolved")
		}
		aliases[i] = identitySpacing(v)
	}
	group, e := normalizeTitle(r.Group)
	if e != nil {
		return r, errors.New("rule_normalization_unresolved")
	}
	r.Aliases = aliases
	r.Group = identitySpacing(group)
	return r, nil
}

// 媒体标题与路径共用分隔符契约；下划线是标点，不沿用 regexp 的 word boundary。
func releaseDelimiter(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}
func hasSpecialEpisode(s string) bool {
	for _, word := range strings.FieldsFunc(s, releaseDelimiter) {
		token := strings.ToUpper(strings.TrimRightFunc(word, unicode.IsDigit))
		switch token {
		case "SP", "OVA", "OAD", "NCOP", "NCED", "SPECIAL", "PV":
			return true
		}
	}
	for _, marker := range []string{"特典", "特别篇", "特別篇", "番外"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// 只消除结构括号/冒号两侧的布局空白，不折叠作品名内部空白或任意标点。
// 输入已完成 HTML/NFKC 规范化；此函数幂等，避免重复 HTML 解码。
func identitySpacing(s string) string {
	out := make([]rune, 0, len(s))
	layout := func(r rune) bool { return unicode.IsSpace(r) || r == '_' || r == '-' || r == '.' }
	for _, r := range s {
		if r == '[' {
			for len(out) > 0 && layout(out[len(out)-1]) {
				out = out[:len(out)-1]
			}
		}
		if r == ']' || r == ':' {
			for len(out) > 0 && unicode.IsSpace(out[len(out)-1]) {
				out = out[:len(out)-1]
			}
		}
		if unicode.IsSpace(r) && len(out) > 0 && (out[len(out)-1] == '[' || out[len(out)-1] == ':') {
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func identityPreview(p TitlePreview) TitlePreview {
	p.Normalized = identitySpacing(p.Normalized)
	p.Group = identitySpacing(p.Group)
	return p
}
