package editor

import (
	"encoding/json"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/lsp"
	"testing"
)

func TestLanguageWorkspaceEditValidation(t *testing.T) {
	a, targets := workspaceFixture(t)
	uri := lsp.URI(targets[0].Buffer.File.Path)
	raw := func(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
	edits := []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{}, End: lsp.Position{Character: 3}}, NewText: "你好"}}
	out, err := a.decodeWorkspaceEdit(raw(map[string]any{"changes": map[string]any{uri: edits}}))
	if err != nil || len(out) != 1 {
		t.Fatal(err)
	}
	a.showWorkspacePreview("rename", out)
	a.applyWorkspacePreview()
	if out[0].Buffer.Doc.Text.String() != "你好" {
		t.Fatal(a.Status)
	}
	_, err = a.decodeWorkspaceEdit(raw(map[string]any{"documentChanges": []any{map[string]any{"textDocument": map[string]any{"uri": uri, "version": 0}, "edits": edits}}}))
	if err == nil {
		t.Fatal("accepted stale version")
	}
	_, err = a.decodeWorkspaceEdit(raw(map[string]any{"documentChanges": []any{map[string]any{"kind": "delete", "uri": uri}}}))
	if err == nil {
		t.Fatal("accepted resource operation")
	}
}
func TestHoverReportIsReadOnlyAndCanClose(t *testing.T) {
	a := testApp(t)
	a.intelligenceResult("hover", nil, json.RawMessage(`{"contents":{"kind":"markdown","value":"hello\nworld"}}`))
	b := a.Current()
	a.insert("bad")
	key(a, tcell.KeyCtrlZ)
	if b.Doc.Text.String() != "hello\nworld" || b.Doc.Dirty() {
		t.Fatal("report mutated")
	}
	a.closeBuffer()
	if a.Current() == b || a.Dialog != nil {
		t.Fatal("close report")
	}
}
func TestSymbolsAndJumpHistory(t *testing.T) {
	a := testApp(t)
	a.insert("abc\ndef")
	b := a.Current()
	b.Doc.Selections[0].Anchor = 0
	b.Doc.Selections[0].Head = 0
	a.jumpTo(b, 5)
	a.jumpBy(-1)
	if b.Doc.Selections[0].Head != 0 {
		t.Fatal("back")
	}
	a.jumpBy(1)
	if b.Doc.Selections[0].Head != 5 {
		t.Fatal("forward")
	}
}
