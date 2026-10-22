package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUnicodeChunksPreserveText(t *testing.T) {
	text := strings.Repeat("鬼火👻", 500)
	for _, limit := range []int{1, 7, 3800} {
		chunks := ChunkText(text, limit)
		for _, chunk := range chunks {
			if !utf8.ValidString(chunk) {
				t.Fatalf("limit %d split a Unicode character", limit)
			}
		}
		if strings.Join(chunks, "") != text {
			t.Fatalf("limit %d lost terminal output", limit)
		}
	}
}

func TestTextPrefixPreservesCharacterBoundaries(t *testing.T) {
	if got := TextPrefix("abc👻tail", 6); got != "abc" {
		t.Fatalf("prefix=%q", got)
	}
	if got := TextPrefix("abc👻tail", 7); got != "abc👻" {
		t.Fatalf("prefix=%q", got)
	}
}
