package editor

import (
	"context"
	"encoding/json"
	"github.com/gdamore/tcell/v2"
	snippets "github.com/gongahkia/shack/internal/snippet"
	"github.com/gongahkia/shack/internal/text"
	"strings"
	"testing"
	"time"
)

func TestSnippetDefaultsChoicesAndRejectMalformed(t *testing.T) {
	path, _, e := parseSnippet(`C:\Users\file $0`)
	if e != nil || path != `C:\Users\file ` {
		t.Fatal(path, e)
	}
	s, stops, err := parseSnippet(`fn(${1:name}, ${2|one,two|}, $1)$0`)
	if err != nil || s != "fn(name, one, name)" || len(stops) != 3 || len(stops[0].Ranges) != 2 {
		t.Fatal(s, stops, err)
	}
	for _, raw := range []string{`${1`, `${1:$1}`, `${1/a/b/z}`} {
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
	snippetUntil(t, a, func() bool { return a.snippet != nil })
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

func snippetUntil(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for !ready() {
		select {
		case update := <-a.updates:
			update()
		case <-deadline.C:
			t.Fatal("snippet timeout:", a.Status)
		}
	}
}
func insertTestSnippet(t *testing.T, a *App, source string) {
	t.Helper()
	b := a.Current()
	selection := b.Doc.Selections[0]
	a.snippetCompletion(b, b.Doc.Version, text.Edit{Start: selection.Start(), End: selection.End(), Text: source}, nil)
	snippetUntil(t, a, func() bool { return a.snippet != nil })
}
func TestAdjacentLinkedSnippetRanges(t *testing.T) {
	for _, source := range []string{`${1}${1}$0`, `${1:x}$1$0`} {
		t.Run(source, func(t *testing.T) {
			a := testApp(t)
			insertTestSnippet(t, a, source)
			a.insert("a")
			a.insert("b")
			if got := a.Current().Doc.Text.String(); got != "abab" {
				t.Fatal(got, a.Status)
			}
			key(a, tcell.KeyBackspace2)
			if got := a.Current().Doc.Text.String(); got != "aa" {
				t.Fatal(got, a.Status)
			}
			key(a, tcell.KeyLeft)
			a.insert("z")
			if got := a.Current().Doc.Text.String(); got != "zaza" {
				t.Fatal(got, a.Status)
			}
			key(a, tcell.KeyTab)
			if a.snippet != nil || a.Current().Doc.Selections[0].Head != 4 {
				t.Fatal("final stop", a.Current().Doc.Selections)
			}
		})
	}
}
func TestNestedSnippetEditingAndParentReplacement(t *testing.T) {
	a := testApp(t)
	insertTestSnippet(t, a, `${1:hi ${2:name}} / $1 / $2 $0`)
	key(a, tcell.KeyTab)
	if len(a.Current().Doc.Selections) != 3 {
		t.Fatal("nested mirrors missing")
	}
	a.insert("world")
	if got := a.Current().Doc.Text.String(); got != "hi world / hi world / world " {
		t.Fatal(got, a.Status)
	}
	key(a, tcell.KeyBacktab)
	a.insert("x")
	if got := a.Current().Doc.Text.String(); got != "x / x / world " {
		t.Fatal(got, a.Status)
	}
	key(a, tcell.KeyTab)
	if len(a.Current().Doc.Selections) != 1 {
		t.Fatal("overwritten nested stops survived")
	}
	a.insert("Y")
	if got := a.Current().Doc.Text.String(); got != "x / x / Y " {
		t.Fatal(got, a.Status)
	}
}
func TestSnippetChoicesAndTransformUndo(t *testing.T) {
	a := testApp(t)
	insertTestSnippet(t, a, `${1|red,green,blue|} ${2:name}$0`)
	if a.Dialog == nil || !a.Dialog.Inline {
		t.Fatal("no choice picker")
	}
	key(a, tcell.KeyDown)
	key(a, tcell.KeyEnter)
	if got := a.Current().Doc.Text.String(); got != "green name" {
		t.Fatal(got)
	}
	key(a, tcell.KeyTab)
	a.insert("value")
	if got := a.Current().Doc.Text.String(); got != "green value" {
		t.Fatal(got)
	}
	a = testApp(t)
	insertTestSnippet(t, a, `${1:foo_bar} ${1/(.*)/${1:/camelcase}/} ${2:x}$0`)
	a.insert("hello_world")
	if got := a.Current().Doc.Text.String(); got != "hello_world fooBar x" {
		t.Fatal(got)
	}
	key(a, tcell.KeyTab)
	snippetUntil(t, a, func() bool {
		return a.snippet != nil && !a.snippet.Pending && a.snippet.Stops[a.snippet.Index].Number == 2
	})
	if got := a.Current().Doc.Text.String(); got != "hello_world helloWorld x" {
		t.Fatal(got, a.Status)
	}
	key(a, tcell.KeyCtrlZ)
	if got := a.Current().Doc.Text.String(); got != "foo_bar fooBar x" {
		t.Fatal("transform split undo history", got)
	}
}
func TestSnippetLateCompletionAndTransform(t *testing.T) {
	a := testApp(t)
	b := a.Current()
	a.snippetCompletion(b, b.Doc.Version, text.Edit{Text: `${1:old}`}, nil)
	a.insert("new")
	select {
	case update := <-a.updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("no completion")
	}
	if b.Doc.Text.String() != "new" || a.snippet != nil {
		t.Fatal("stale completion applied")
	}
	a = testApp(t)
	insertTestSnippet(t, a, `${1:a} ${1/(.*)/${1:/upcase}/}$0`)
	a.insert("b")
	key(a, tcell.KeyTab)
	a.insert("c")
	snippetUntil(t, a, func() bool { return a.snippet != nil && !a.snippet.Pending })
	if got := a.Current().Doc.Text.String(); got != "bc A" {
		t.Fatal(got)
	}
	key(a, tcell.KeyTab)
	snippetUntil(t, a, func() bool { return a.snippet == nil })
	if got := a.Current().Doc.Text.String(); got != "bc BC" {
		t.Fatal(got, a.Status)
	}
}
func TestSnippetContextVariablesAndBounds(t *testing.T) {
	source := text.New("hello 你好_world")
	vars := snippetVariables(source, text.Selection{Anchor: 6, Head: source.Len()}, "/project/main.py", "/project", "clip", time.Date(2026, 10, 25, 12, 34, 56, 789000000, time.UTC))
	for name, want := range map[string]string{"TM_SELECTED_TEXT": "你好_world", "TM_CURRENT_WORD": "你好_world", "TM_FILENAME_BASE": "main", "LINE_COMMENT": "#", "CURRENT_YEAR": "2026", "CURRENT_MILLISECOND": "789", "CLIPBOARD": "clip"} {
		if vars[name] != want {
			t.Fatal(name, vars[name], want)
		}
	}
	if len(vars["UUID"]) != 36 || len(vars["RANDOM"]) != 6 || len(vars["RANDOM_HEX"]) != 6 {
		t.Fatal("random variables")
	}
	vars = snippetVariables(text.New("value\r\n"), text.Selection{}, "/project/.env", "/project", "", time.Now())
	if vars["TM_FILENAME_BASE"] != ".env" || vars["TM_CURRENT_LINE"] != "value" {
		t.Fatal("dotfile or CRLF context", vars)
	}
	large := text.New(strings.Repeat("a", snippets.MaxExpansion+100))
	vars = snippetVariables(large, text.Selection{}, "", "", "", time.Now())
	p, _ := snippets.Compile(`$TM_CURRENT_LINE`)
	p, _ = p.Resolve(vars)
	if _, err := p.Expand(context.Background(), nil, nil, true); err == nil {
		t.Fatal("oversized variable was silently truncated")
	}
}
