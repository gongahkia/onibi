package editor

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/testutil"
)

func TestActionFixtureProcess(t *testing.T) {
	if os.Getenv("SHACK_EDITOR_ACTION_FIXTURE") != "1" {
		return
	}
	testutil.ActionServer()
	os.Exit(0)
}
func actionFixture(t *testing.T, a *App) *lsp.Client {
	t.Helper()
	t.Setenv("SHACK_EDITOR_ACTION_FIXTURE", "1")
	state := &languageState{}
	a.languages = map[string]*languageState{"fixture": state}
	c, err := lsp.Start(a.ctx, []string{os.Args[0], "-test.run=^TestActionFixtureProcess$"}, a.Root, nil, func(ctx context.Context, p lsp.ApplyEditParams) lsp.ApplyEditResult {
		return a.reviewServerEdit(ctx, "fixture", state, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	state.Client = c
	return c
}
func actionUntil(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ready() {
		select {
		case update := <-a.updates:
			update()
		case <-deadline:
			t.Fatalf("action timed out: %s", a.Status)
		}
	}
}
func actionEdit(path, replacement string) any {
	return map[string]any{"changes": map[string]any{lsp.URI(path): []lsp.TextEdit{{Range: lsp.Range{End: lsp.Position{Character: 3}}, NewText: replacement}}}}
}
func TestResolvedServerActionReviewAndUndo(t *testing.T) {
	a, targets := workspaceFixture(t)
	c := actionFixture(t, a)
	path := targets[0].Buffer.File.Path
	raw, _ := json.Marshal([]any{map[string]any{"title": "Fix", "kind": "quickfix", "customField": "preserved", "data": map[string]any{"label": "review fix", "edit": actionEdit(path, "new")}}})
	a.codeActions(c, raw)
	key(a, tcell.KeyEnter)
	actionUntil(t, a, func() bool { return a.Preview != nil })
	if !a.Preview.Strict || a.Preview.Server != c {
		t.Fatal("not a reviewed whole action")
	}
	a.key(tcell.NewEventKey(tcell.KeyRune, ' ', 0))
	if !a.Preview.Checked[0] {
		t.Fatal("action could be partially applied")
	}
	key(a, tcell.KeyEnter)
	actionUntil(t, a, func() bool { return a.languageCommand == nil })
	var edited *Buffer
	for _, b := range a.Buffers {
		if b.File.Path == path {
			edited = b
		}
	}
	if edited == nil || edited.Doc.Text.String() != "new" {
		t.Fatal(a.Status)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "one" {
		t.Fatal("wrote without save")
	}
	a.undoWorkspace(false)
	if edited.Doc.Text.String() != "one" {
		t.Fatal("undo failed")
	}
}
func TestEditThenCommandAndCancelledPreview(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{true: "accept", false: "cancel"}[accept], func(t *testing.T) {
			a, targets := workspaceFixture(t)
			c := actionFixture(t, a)
			path := targets[0].Buffer.File.Path
			raw, _ := json.Marshal([]any{map[string]any{"title": "Two-step fix", "edit": actionEdit(path, "two"), "command": map[string]any{"command": "fixture.apply", "arguments": []any{map[string]any{"edit": actionEdit(path, "new")}}}}})
			a.codeActions(c, raw)
			key(a, tcell.KeyEnter)
			if a.Preview == nil || a.languageCommand != nil {
				t.Fatal("command ran before edit accepted")
			}
			if !accept {
				key(a, tcell.KeyEscape)
				if a.languageCommand != nil || len(a.Buffers) != 1 {
					t.Fatal("cancel applied action")
				}
				return
			}
			key(a, tcell.KeyEnter)
			actionUntil(t, a, func() bool { return a.Preview != nil })
			key(a, tcell.KeyEscape)
			actionUntil(t, a, func() bool { return a.languageCommand == nil })
			if a.Buffers[1].Doc.Text.String() != "two" {
				t.Fatal("rejected callback changed accepted first edit")
			}
		})
	}
}
func TestServerEditCancellationAndBusyDialog(t *testing.T) {
	for _, mode := range []string{"server_cancel", "command_cancel", "dialog", "closed", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			a, targets := workspaceFixture(t)
			c := actionFixture(t, a)
			params, _ := json.Marshal([]any{map[string]any{"edit": actionEdit(targets[0].Buffer.File.Path, "new")}})
			method := "fixture.apply"
			if mode == "server_cancel" {
				method = "fixture.cancel"
			}
			if mode == "dialog" {
				a.prompt("Keep this dialog", "", func(string) {})
			}
			a.executeServerCommand(c, "Fix", serverCommand{method, params})
			if mode == "dialog" || mode == "server_cancel" {
				actionUntil(t, a, func() bool { return a.languageCommand == nil })
			} else {
				actionUntil(t, a, func() bool { return a.Preview != nil })
				switch mode {
				case "command_cancel":
					a.cancelLanguageCommand()
				case "closed":
					c.Close()
					actionUntil(t, a, func() bool { return a.Preview == nil })
				case "replaced":
					a.prompt("Another command", "", func(string) {})
				}
				actionUntil(t, a, func() bool { return a.languageCommand == nil })
			}
			if a.Preview != nil || len(a.Buffers) != 1 {
				t.Fatal("cancelled edit survived", a.Status)
			}
		})
	}
}
func TestCodeActionRejectsClosedSourceAndMalformedCommand(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Open(targets[0].Buffer.File.Path)
	raw, _ := json.Marshal([]any{map[string]any{"title": "fix", "edit": actionEdit(targets[0].Buffer.File.Path, "new")}})
	a.codeActions(nil, raw)
	run := a.Dialog.Items[0].Run
	a.Dialog = nil
	a.closeBuffer()
	run()
	if a.Preview != nil {
		t.Fatal("closed source accepted")
	}
	for _, value := range []string{`{"command":"","arguments":[]}`, `{"command":"fix","arguments":{}}`, `123`} {
		if _, err := actionServerCommand(codeAction{Command: json.RawMessage(value)}); err == nil {
			t.Fatal("malformed command", value)
		}
	}
}
