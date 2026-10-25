package editor

import (
	"encoding/json"
	"github.com/gdamore/tcell/v2"
	"testing"
)

func TestInlineCompletionLetsTypingContinue(t *testing.T) {
	a := testApp(t)
	a.insert("he")
	b := a.Current()
	a.completions(b, b.Doc.Version, 2, "he", json.RawMessage(`[{"label":"hello"}]`))
	a.draw()
	a.Handle(tcell.NewEventKey(tcell.KeyRune, 'l', 0))
	if b.Doc.Text.String() != "hel" || a.Dialog != nil {
		t.Fatal("completion swallowed typing")
	}
	a.completions(b, b.Doc.Version, 3, "hel", json.RawMessage(`[{"label":"hello"}]`))
	key(a, tcell.KeyTab)
	if b.Doc.Text.String() != "hello" {
		t.Fatal(b.Doc.Text.String())
	}
}
