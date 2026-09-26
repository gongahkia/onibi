package telegram

import "strings"

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
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], "\n")
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
