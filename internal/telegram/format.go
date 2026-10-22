package telegram

import (
	"strings"
	"unicode/utf8"
)

// TextPrefix limits text by bytes without splitting a UTF-8 character.
func TextPrefix(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

func ChunkText(s string, max int) []string {
	if max <= 0 {
		max = 3800
	}
	if strings.TrimSpace(s) == "" {
		return []string{"(no output)"}
	}
	var out []string
	for len(s) > max {
		cut := max
		if i := strings.LastIndexByte(s[:max], '\n'); i > max/2 {
			cut = i
		}
		cut = len(TextPrefix(s, cut))
		if cut == 0 {
			// A caller-supplied limit smaller than one character must still
			// make progress without corrupting that character.
			_, cut = utf8.DecodeRuneInString(s)
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], "\n")
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
