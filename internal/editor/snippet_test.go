package editor

import (
	"encoding/json"
	"github.com/gdamore/tcell/v2"
	"testing"
)

func TestSnippetDefaultsChoicesAndRejectUnsupported(t *testing.T) {
	s, stops, err := parseSnippet(`fn(${1:name}, ${2|one,two|}, $1)$0`)
	if err != nil || s != "fn(name, one, name)" || len(stops) != 3 || len(stops[0].Ranges) != 2 {
		t.Fatal(s, stops, err)
	}
	for _, raw := range []string{`${VAR}`, `${1:${2:nested}}`, `${1/a/b/}`} {
		if _, _, err := parseSnippet(raw); err == nil {
			t.Fatal(raw)
		}
	}
}
func TestSnippetCompletionLinkedStopsAndAdditionalEdits(t *testing.T) {
	a := testApp(t)
	a.insert("//file\nfu")
	b := a.Current()
	raw := json.RawMessage(`[{"label":"func","insertTextFormat":2,"insertText":"func(${1:name}, $1)$0","additionalTextEdits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"//import\n"}]}]`)
	a.completions(b, b.Doc.Version, b.Doc.Selections[0].Head, b.Doc.Text.String(), raw)
	a.Dialog.Items[0].Run()
	a.Dialog = nil
	if a.snippet == nil || len(b.Doc.Selections) != 2 {
		t.Fatal(a.Status)
	}
	a.insert("x")
	if b.Doc.Text.String() != "//import\n//file\nfunc(x, x)" {
		t.Fatal(b.Doc.Text.String())
	}
	key(a, tcell.KeyTab)
	if a.snippet != nil || b.Doc.Selections[0].Head != b.Doc.Text.Len() {
		t.Fatal(b.Doc.Selections)
	}
	key(a, tcell.KeyCtrlZ)
	if b.Doc.Text.String() != "//import\n//file\nfunc(name, name)" {
		t.Fatal("snippet undo")
	}
}
