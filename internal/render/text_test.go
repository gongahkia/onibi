package render

import (
	"strings"
	"testing"
)

func TestStripANSIBasic(t *testing.T) {
	in := []byte("\x1b[31mhello\x1b[0m world")
	got := StripANSI(in)
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestStripANSIOSC(t *testing.T) {
	// OSC sequence: ESC ] 0 ; title BEL
	in := []byte("\x1b]0;some title\x07after")
	got := StripANSI(in)
	if string(got) != "after" {
		t.Fatalf("got %q", got)
	}
}

func TestStripANSICursorMove(t *testing.T) {
	in := []byte("\x1b[2J\x1b[Hgreen\x1b[5;10Hred")
	got := string(StripANSI(in))
	if !strings.Contains(got, "green") || !strings.Contains(got, "red") {
		t.Fatalf("got %q", got)
	}
}

func TestTextTailEmpty(t *testing.T) {
	out := TextTail(nil, Options{})
	if !strings.Contains(out, "(no output)") {
		t.Fatalf("got %q", out)
	}
}

func TestTextTailFencing(t *testing.T) {
	out := TextTail([]byte("line1\nline2\n"), Options{})
	if !strings.HasPrefix(out, "```") {
		t.Fatalf("missing leading fence: %q", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "```") {
		t.Fatalf("missing trailing fence: %q", out)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Fatalf("body missing lines: %q", out)
	}
}

func TestTextTailMaxLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line\n")
	}
	out := TextTail([]byte(b.String()), Options{MaxLines: 5})
	if strings.Count(out, "line") < 5 {
		t.Fatalf("expected 5 line entries, got %d in %q", strings.Count(out, "line"), out)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation marker: %q", out)
	}
}

func TestTextTailANSIStripped(t *testing.T) {
	out := TextTail([]byte("\x1b[31mERROR\x1b[0m: boom"), Options{})
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("ANSI leaked: %q", out)
	}
	if !strings.Contains(out, "ERROR: boom") {
		t.Fatalf("body wrong: %q", out)
	}
}

func TestTextTailLanguageHint(t *testing.T) {
	out := TextTail([]byte("ok"), Options{Lang: "shell"})
	if !strings.HasPrefix(out, "```shell\n") {
		t.Fatalf("expected shell hint, got %q", out[:20])
	}
}
