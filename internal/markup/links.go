package markup

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// LinkTarget accepts editor navigation targets separately from OSC 8 URLs.
// Local paths are never passed to a terminal hyperlink or a command interpreter.
func LinkTarget(s string) string {
	if s == "" || len(s) > 2048 {
		return ""
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return ""
		}
	}
	if SafeURL(s) != "" {
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "\\") {
		return ""
	}
	for _, c := range u.Path + u.Fragment {
		if unicode.IsControl(c) {
			return ""
		}
	}
	return s
}

func (r *renderer) setAnchor(key string, offset int) {
	if r.anchors == nil {
		r.anchors = map[string]int{}
	}
	r.anchors[key] = offset
}

// Heading slugs retain Unicode letters/marks/numbers, hyphens and underscores,
// remove punctuation, and suffix duplicates without colliding with explicit -N.
func (r *renderer) headingAnchor(title string, offset int) {
	base := strings.Map(func(c rune) rune {
		switch {
		case unicode.IsSpace(c):
			return '-'
		case unicode.IsLetter(c), unicode.IsNumber(c), unicode.IsMark(c), c == '-', c == '_':
			return unicode.ToLower(c)
		default:
			return -1
		}
	}, title)
	if r.slugs == nil {
		r.slugs = map[string]int{}
	}
	name := base
	for {
		if _, exists := r.anchors["heading:"+name]; !exists {
			break
		}
		r.slugs[base]++
		name = fmt.Sprintf("%s-%d", base, r.slugs[base])
	}
	r.setAnchor("heading:"+name, offset)
}
