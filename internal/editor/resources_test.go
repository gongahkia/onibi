package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/lsp"
)

func resourceWire(steps ...any) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"documentChanges": steps})
	return raw
}
func createResource(path string) any { return map[string]any{"kind": "create", "uri": lsp.URI(path)} }
func renameResource(from, to string) any {
	return map[string]any{"kind": "rename", "oldUri": lsp.URI(from), "newUri": lsp.URI(to)}
}
func deleteResource(path string) any { return map[string]any{"kind": "delete", "uri": lsp.URI(path)} }
func editResource(path string, start, end int, value string) any {
	return map[string]any{"textDocument": map[string]any{"uri": lsp.URI(path), "version": nil}, "edits": []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{Character: start}, End: lsp.Position{Character: end}}, NewText: value}}}
}
func waitResourcePreview(t *testing.T, a *App, raw json.RawMessage) {
	t.Helper()
	a.workspaceEdit("Resources", raw)
	actionUntil(t, a, func() bool { return a.resourcePreparation == nil })
	if a.Preview == nil {
		t.Fatal(a.Status)
	}
}
func acceptResources(t *testing.T, a *App) {
	t.Helper()
	key(a, tcell.KeyEnter)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if a.batch == nil || a.batch.Resources == nil {
		t.Fatal(a.Status)
	}
}
func resourceBufferAt(a *App, path string) *Buffer {
	for _, b := range a.Buffers {
		if b.File.Path == path {
			return b
		}
	}
	return nil
}

func TestResourceCreateEditSaveUndoRedo(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	path := filepath.Join(a.Root, "new/sub/file.txt")
	waitResourcePreview(t, a, resourceWire(createResource(path), editResource(path, 0, 0, "hello 😀\n")))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preview wrote a file")
	}
	acceptResources(t, a)
	b := resourceBufferAt(a, path)
	if b == nil || b.Doc.Text.String() != "hello 😀\n" || !b.Doc.Dirty() {
		t.Fatal(a.Status)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatal("create should not save later buffer edits", string(data), err)
	}
	a.saveAll(nil)
	data, _ = os.ReadFile(path)
	if string(data) != "hello 😀\n" {
		t.Fatal(a.Status)
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("undo left created file", a.Status)
	}
	if !b.Transient || b.File.Path != "" {
		t.Fatal("undo left writable buffer pointing at deleted file")
	}
	a.undoWorkspace(true)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if b.File.Path != path || b.Transient || b.Doc.Text.String() != "hello 😀\n" {
		t.Fatal("redo", a.Status)
	}
	a.saveAll(nil)
	data, _ = os.ReadFile(path)
	if string(data) != "hello 😀\n" {
		t.Fatal(a.Status)
	}
}
func TestResourceRenameDirtyBufferAndDeletePreservesText(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	old := targets[0].Buffer.File.Path
	next := filepath.Join(a.Root, "renamed.txt")
	a.Open(old)
	b := a.Current()
	a.insert("dirty ")
	waitResourcePreview(t, a, resourceWire(renameResource(old, next), editResource(next, 6, 9, "new")))
	acceptResources(t, a)
	if b.File.Path != next || b.Doc.Text.String() != "dirty new" || !b.Doc.Dirty() {
		t.Fatal(b.File.Path, b.Doc.Text.String(), a.Status)
	}
	data, _ := os.ReadFile(next)
	if string(data) != "one" {
		t.Fatal("rename saved dirty text")
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if b.File.Path != old || b.Doc.Text.String() != "dirty one" || !b.Doc.Dirty() {
		t.Fatal("undo lost dirty original", a.Status)
	}
	waitResourcePreview(t, a, resourceWire(deleteResource(old)))
	acceptResources(t, a)
	if !b.Transient || b.Doc.Text.String() != "dirty one" {
		t.Fatal("deleted buffer lost its text")
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if b.Transient || b.File.Path != old || b.Doc.Text.String() != "dirty one" || !b.Doc.Dirty() {
		t.Fatal("delete undo", a.Status)
	}
}
func TestClosedResourceRenameThenEditUndoesToOriginalPath(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	old := targets[0].Buffer.File.Path
	next := filepath.Join(a.Root, "renamed.txt")
	waitResourcePreview(t, a, resourceWire(renameResource(old, next), editResource(next, 0, 3, "new")))
	acceptResources(t, a)
	b := resourceBufferAt(a, next)
	if b == nil {
		t.Fatal(a.Status)
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if b.File.Path != old || b.Doc.Text.String() != "one" {
		t.Fatal("wrong original buffer", b.File.Path, a.Status)
	}
}
func TestResourcePreviewStaleAndCancelled(t *testing.T) {
	for _, mode := range []string{"cancel", "disk", "buffer", "open"} {
		t.Run(mode, func(t *testing.T) {
			a, targets := workspaceFixture(t)
			a.Root = filepath.Dir(targets[0].Buffer.File.Path)
			old := targets[0].Buffer.File.Path
			next := filepath.Join(a.Root, "new.txt")
			if mode == "buffer" {
				a.Open(old)
			}
			waitResourcePreview(t, a, resourceWire(renameResource(old, next)))
			switch mode {
			case "cancel":
				key(a, tcell.KeyEscape)
			case "disk":
				os.WriteFile(old, []byte("external"), 0600)
				key(a, tcell.KeyEnter)
				actionUntil(t, a, func() bool { return a.Preview == nil })
			case "buffer":
				a.Current().Doc.Insert("edit")
				key(a, tcell.KeyEnter)
			case "open":
				a.Open(old)
				key(a, tcell.KeyEnter)
				if a.Preview != nil && a.Preview.CommitCancel != nil {
					actionUntil(t, a, func() bool { return a.Preview == nil })
				}
			}
			if _, err := os.Stat(next); !os.IsNotExist(err) {
				t.Fatal("stale or cancelled preview mutated disk", a.Status)
			}
			if a.Preview != nil {
				a.cancelPreview("test cleanup")
			}
		})
	}
}
func TestServerResourceEditReviewAndReply(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	c := actionFixture(t, a)
	path := filepath.Join(a.Root, "callback.txt")
	params, _ := json.Marshal([]any{map[string]any{"edit": resourceWire(createResource(path), editResource(path, 0, 0, "callback"))}})
	a.executeServerCommand(c, "Create", serverCommand{"fixture.apply", params})
	actionUntil(t, a, func() bool { return a.Preview != nil })
	acceptResources(t, a)
	actionUntil(t, a, func() bool { return a.languageCommand == nil })
	if resourceBufferAt(a, path) == nil {
		t.Fatal(a.Status)
	}
}

func TestResourceDirectoryReviewAndUnsavedChildRename(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	from, to := filepath.Join(a.Root, "old"), filepath.Join(a.Root, "new")
	os.Mkdir(from, 0755)
	os.WriteFile(filepath.Join(from, "binary"), []byte{0, 255}, 0600)
	a.Open(filepath.Join(from, "unsaved.txt"))
	b := a.Current()
	a.insert("unsaved")
	waitResourcePreview(t, a, resourceWire(renameResource(from, to)))
	acceptResources(t, a)
	if b.File.Path != filepath.Join(to, "unsaved.txt") || b.File.exists || !b.Doc.Dirty() {
		t.Fatal("unsaved child did not move", a.Status)
	}
	a.saveAll(nil)
	deleteDir := map[string]any{"kind": "delete", "uri": lsp.URI(to), "options": map[string]bool{"recursive": true}}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if b.File.Path != filepath.Join(from, "unsaved.txt") || b.File.exists || !b.Doc.Dirty() {
		t.Fatal("rename undo after saving a previously missing child", a.Status)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatal("renamed directory survived undo", a.Status)
	}
	a.undoWorkspace(true)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	a.saveAll(nil)
	waitResourcePreview(t, a, resourceWire(deleteDir))
	if len(a.Preview.Labels) != 3 {
		t.Fatal("recursive preview hid file contents", a.Preview.Labels)
	}
	acceptResources(t, a)
	if !b.Transient || b.Doc.Text.String() != "unsaved" {
		t.Fatal(a.Status)
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	data, err := os.ReadFile(filepath.Join(to, "binary"))
	if err != nil || len(data) != 2 || data[1] != 255 {
		t.Fatal("binary directory member lost", err)
	}
}

func TestResourceOverwriteRetainsBothBuffersAndUndo(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	from, to := targets[0].Buffer.File.Path, targets[1].Buffer.File.Path
	a.Open(from)
	source := a.Current()
	a.insert("source ")
	a.Open(to)
	destination := a.Current()
	a.insert("destination ")
	step := map[string]any{"kind": "rename", "oldUri": lsp.URI(from), "newUri": lsp.URI(to), "options": map[string]bool{"overwrite": true, "ignoreIfExists": true}}
	waitResourcePreview(t, a, resourceWire(step))
	acceptResources(t, a)
	if source.File.Path != to || source.Doc.Text.String() != "source one" || !destination.Transient || destination.Doc.Text.String() != "destination one" {
		t.Fatal(a.Status)
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if source.File.Path != from || destination.File.Path != to || destination.Transient || !destination.Doc.Dirty() {
		t.Fatal("overwritten buffer did not restore", a.Status)
	}
}

func TestSequentialTextDocumentEditsKeepOrdinaryWorkspaceUndo(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	path := targets[0].Buffer.File.Path
	waitResourcePreview(t, a, resourceWire(editResource(path, 0, 3, "two"), editResource(path, 0, 3, "new")))
	key(a, tcell.KeyEnter)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	b := resourceBufferAt(a, path)
	if b == nil || b.Doc.Text.String() != "new" || a.batch.Resources != nil {
		t.Fatal(a.Status)
	}
	a.saveAll(nil)
	a.undoWorkspace(false)
	if b.Doc.Text.String() != "one" {
		t.Fatal("sequential edits did not undo together", a.Status)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatal("text-only undo changed disk")
	}
}

func TestResourcePreparationCancellationAndOrderlyShutdown(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	path := filepath.Join(a.Root, "shutdown.txt")
	a.workspaceEdit("Create", resourceWire(createResource(path)))
	key(a, tcell.KeyEscape)
	if a.resourcePreparation != nil {
		t.Fatal("preparation was not cancelled")
	}
	waitResourcePreview(t, a, resourceWire(createResource(path), editResource(path, 0, 0, "retained")))
	key(a, tcell.KeyEnter)
	a.closeResourceWork()
	if a.resourceResult != nil || a.Preview != nil {
		t.Fatal("shutdown left an unfinished resource commit")
	}
	_, err := os.Stat(path)
	if err == nil {
		b := resourceBufferAt(a, path)
		if b == nil || b.Doc.Text.String() != "retained" {
			t.Fatal("committed filesystem state was not published before recovery shutdown")
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestAnnotatedTextEditsAndDocumentChangesPrecedence(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.Root = filepath.Dir(targets[0].Buffer.File.Path)
	path := targets[0].Buffer.File.Path
	edits := []lsp.TextEdit{{Range: lsp.Range{End: lsp.Position{Character: 3}}, NewText: "new", AnnotationID: "review"}}
	wire := map[string]any{"changes": map[string]any{lsp.URI(path): edits}, "changeAnnotations": map[string]any{"review": map[string]any{"label": "Update public API", "description": "Changes callers too", "needsConfirmation": true}}}
	raw, _ := json.Marshal(wire)
	waitResourcePreview(t, a, raw)
	if len(a.Preview.Labels) != 2 || a.Preview.Labels[0] != "Annotation: Update public API" {
		t.Fatal("annotation not reviewed", a.Preview.Labels)
	}
	key(a, tcell.KeyEscape)
	delete(wire, "changeAnnotations")
	raw, _ = json.Marshal(wire)
	a.workspaceEdit("invalid", raw)
	actionUntil(t, a, func() bool { return a.resourcePreparation == nil })
	if a.Preview != nil {
		t.Fatal("unknown annotation accepted")
	}
	wire = map[string]any{"changes": map[string]any{lsp.URI(path): edits}, "documentChanges": []any{editResource(path, 0, 3, "preferred")}}
	raw, _ = json.Marshal(wire)
	targets, err := a.decodeWorkspaceEdit(raw)
	if err != nil || len(targets) != 1 || targets[0].Edits[0].Text != "preferred" {
		t.Fatal("documentChanges not preferred", err)
	}
}
