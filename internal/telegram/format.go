package telegram

import (
	"html"
	"strings"
	"unicode/utf8"
)

const SafeTextLimit = 3500

func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

func HTMLPre(body string) string {
	return "<pre>" + html.EscapeString(body) + "</pre>"
}

func HTMLCode(label, body string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return HTMLPre(body)
	}
	return html.EscapeString(label) + "\n" + HTMLPre(body)
}

func SplitForTelegram(s string, limit int) []string {
	if limit <= 0 {
		limit = SafeTextLimit
	}
	if len(s) <= limit {
		return []string{s}
	}
	var out []string
	for len(s) > limit {
		cut := limit
		if i := strings.LastIndexByte(s[:limit], '\n'); i > limit/2 {
			cut = i + 1
		} else {
			for !utf8.RuneStart(s[cut]) && cut > 0 {
				cut--
			}
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
