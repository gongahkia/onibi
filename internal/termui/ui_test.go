package termui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRattleElapsedDirectionAndEmpty(t *testing.T) {
	r := Rattle{Frames: []string{"a", "b", "c"}, Interval: 100 * time.Millisecond, Offset: 1}
	if r.Frame(0) != "b" || r.Frame(200*time.Millisecond) != "a" {
		t.Fatal("offset/wrap")
	}
	r.Reverse = true
	if r.Frame(0) != "b" || r.Frame(100*time.Millisecond) != "a" {
		t.Fatal("reverse")
	}
	if (Rattle{}).Frame(time.Second) != "" {
		t.Fatal("empty")
	}
}
func TestPlainProgressHasNoEscapesAndKeepsFailure(t *testing.T) {
	var out bytes.Buffer
	u := New(&out)
	want := errors.New("compiler failed")
	if err := u.Step("Compile", func(update func(string)) error { update("package"); return want }); err != want {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "[failed] Compile") {
		t.Fatal(out.String())
	}
}
func TestInteractiveProgressStopsBeforeNextOutput(t *testing.T) {
	var out bytes.Buffer
	u := &UI{Out: &out, Interactive: true, width: 32}
	if err := u.Step("Compile", func(update func(string)) error {
		update(strings.Repeat("界", 80))
		time.Sleep(110 * time.Millisecond)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	u.Line("done")
	before := out.String()
	time.Sleep(120 * time.Millisecond)
	if out.String() != before {
		t.Fatal("spinner survived completion")
	}
	if !strings.HasSuffix(before, "done\n") {
		t.Fatal(before)
	}
}
func TestDisplaySanitizesTerminalControlsAndClipsCells(t *testing.T) {
	if strings.ContainsAny(Clean("x\x1b\ny\r\t"), "\x1b\n\r\t") {
		t.Fatal("control injection")
	}
	if clip("界界界界", 6) != "界..." {
		t.Fatal("wide cells clipped incorrectly")
	}
}
