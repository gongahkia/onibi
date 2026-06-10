package render

import (
	"regexp"
	"strings"
)

type Mode string

const (
	ModeAuto Mode = "auto"
	ModeText Mode = "text"
	ModePNG  Mode = "png"
)

var absoluteMoveRE = regexp.MustCompile(`\x1b\[[0-9]{1,3};[0-9]{1,3}[Hf]`)

func DetectMode(buf []byte) Mode {
	s := string(buf)
	if containsAny(s, "\x1b[?1049h", "\x1b[?1049l", "\x1b[H", "\x1b[s", "\x1b7") {
		return ModePNG
	}
	if len(absoluteMoveRE.FindAllStringIndex(s, 3)) >= 2 {
		return ModePNG
	}
	return ModeText
}

func ResolveMode(buf []byte, override Mode) Mode {
	switch override {
	case ModeText, ModePNG:
		return override
	default:
		return DetectMode(buf)
	}
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
}
