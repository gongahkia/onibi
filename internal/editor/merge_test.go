package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/gitmerge"
	"github.com/gongahkia/shack/internal/testutil"
)

func mergeEditor(t *testing.T) (*App, *Buffer) {
	t.Helper()
	a := testApp(t)
	a.Root = testutil.ConflictedRepository(t)
	if err := a.Open(filepath.Join(a.Root, "conflict file.txt")); err != nil {
		t.Fatal(err)
	}
	b := a.Current()
	a.mergeFileActions()
	actionUntil(t, a, func() bool { return a.Dialog != nil })
	return a, b
}
func chooseMerge(t *testing.T, a *App, prefix string) {
	t.Helper()
	if a.Dialog == nil {
		t.Fatal("no dialog", a.Status)
	}
	for _, item := range a.Dialog.Items {
		if strings.HasPrefix(item.Label, prefix) {
			a.Dialog = nil
			item.Run()
			return
		}
	}
	t.Fatal("missing choice", prefix, a.Dialog.Items)
}
func TestMergePreviewUndoSaveAndStage(t *testing.T) {
	a, b := mergeEditor(t)
	original := b.Doc.Text.String()
	chooseMerge(t, a, "Resolve hunk 1")
	chooseMerge(t, a, "Preview both:")
	if a.Preview == nil {
		t.Fatal(a.Status)
	}
	data, _ := os.ReadFile(b.File.Path)
	if string(data) != original {
		t.Fatal("preview wrote disk")
	}
	key(a, tcell.KeyEnter)
	want := "header\nours 😀\ntheirs 🦦\nfooter\n"
	if b.Doc.Text.String() != want {
		t.Fatal(b.Doc.Text.String())
	}
	key(a, tcell.KeyCtrlZ)
	if b.Doc.Text.String() != original {
		t.Fatal("merge edit did not undo")
	}
	key(a, tcell.KeyCtrlY)
	a.Save()
	a.stageMergeResolution()
	actionUntil(t, a, func() bool { return a.mergeCancel == nil })
	if !strings.Contains(a.Status, "Staged resolution") {
		t.Fatal(a.Status)
	}
	if staged := testutil.Git(t, a.Root, "show", ":conflict file.txt"); staged != want {
		t.Fatal(staged)
	}
	if unmerged := testutil.Git(t, a.Root, "ls-files", "-u"); unmerged != "" {
		t.Fatal(unmerged)
	}
	if _, err := os.Stat(filepath.Join(a.Root, ".git/MERGE_HEAD")); err != nil {
		t.Fatal("committed/aborted merge")
	}
}
func TestMergeStagingRejectsMarkersDirtyAndExternalWrites(t *testing.T) {
	a, b := mergeEditor(t)
	a.Dialog = nil
	a.stageMergeResolution()
	actionUntil(t, a, func() bool { return a.mergeCancel == nil })
	if !strings.Contains(a.Status, "resolve all marked conflicts") {
		t.Fatal(a.Status)
	}
	hunks, _ := gitmerge.Parse(b.Doc.Text.String())
	a.previewMergeResolution(b, b.Doc.Text.String(), b.Doc.Version, hunks, "ours")
	key(a, tcell.KeyEnter)
	a.stageMergeResolution()
	if !strings.Contains(a.Status, "Save") {
		t.Fatal(a.Status)
	}
	a.Save()
	os.WriteFile(b.File.Path, []byte("external\n"), 0600)
	a.stageMergeResolution()
	actionUntil(t, a, func() bool { return a.mergeCancel == nil })
	if !strings.Contains(a.Status, "changed on disk") {
		t.Fatal(a.Status)
	}
	if unmerged := testutil.Git(t, a.Root, "ls-files", "-u"); unmerged == "" {
		t.Fatal("staged invalid resolution")
	}
}
func TestMergeStageReportsAndMalformedWholeFileRecovery(t *testing.T) {
	a, b := mergeEditor(t)
	a.Dialog = nil
	a.inspectMergeStage(1)
	actionUntil(t, a, func() bool { return a.mergeCancel == nil })
	if !a.Current().Transient || a.Current().Doc.Text.String() != "header\nbase α\nfooter\n" {
		t.Fatal(a.Status)
	}
	a.insert("bad")
	if a.Current().Doc.Dirty() {
		t.Fatal("stage report was editable")
	}
	a.Open(b.File.Path)
	a.insert("<<<<<<< broken\n")
	a.mergeFileActions()
	actionUntil(t, a, func() bool { return a.Dialog != nil })
	chooseMerge(t, a, "Preview replacing entire file with ours")
	actionUntil(t, a, func() bool { return a.Preview != nil })
	key(a, tcell.KeyEnter)
	if b.Doc.Text.String() != "header\nours 😀\nfooter\n" {
		t.Fatal(a.Status, b.Doc.Text.String())
	}
}
func TestMergeContextAndSnapshotStaleness(t *testing.T) {
	a, b := mergeEditor(t)
	chooseMerge(t, a, "Resolve hunk 1")
	var run func()
	for _, item := range a.Dialog.Items {
		if strings.HasPrefix(item.Label, "Preview ours") {
			run = item.Run
		}
	}
	a.Dialog = nil
	a.insert("edit")
	run()
	if a.Preview != nil {
		t.Fatal("stale hunk preview")
	}
	a.Current().Doc.Undo()
	entry := a.mergeFile.Entry
	testutil.Git(t, a.Root, "add", "--", b.File.Path)
	a.replaceMergeStage(2)
	actionUntil(t, a, func() bool { return a.mergeCancel == nil })
	if a.Preview != nil {
		t.Fatal("stale Git stage snapshot replaced the file")
	}
	if err := a.mergeFile.Repo.Current(context.Background(), entry); err == nil {
		t.Fatal("fixture did not change index")
	}
}

func TestMergeDeletionPreviewUndoAndExplicitStage(t *testing.T) {
	a := testApp(t)
	a.Root = testutil.DeletionConflictRepository(t)
	path := filepath.Join(a.Root, "conflict file.txt")
	a.Open(path)
	b := a.Current()
	a.mergeFileActions()
	actionUntil(t, a, func() bool { return a.Dialog != nil })
	chooseMerge(t, a, "Preview resolving with deletion")
	actionUntil(t, a, func() bool { return a.Preview != nil })
	if _, err := os.Stat(path); err != nil {
		t.Fatal("preview deleted file")
	}
	acceptResources(t, a)
	if !b.Transient || b.Doc.Text.String() != "ours modified\n" {
		t.Fatal(a.Status)
	}
	a.undoWorkspace(false)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	if _, err := os.Stat(path); err != nil {
		t.Fatal("undo did not restore deletion")
	}
	a.undoWorkspace(true)
	actionUntil(t, a, func() bool { return a.Preview == nil })
	a.stageMergeResolution()
	actionUntil(t, a, func() bool { return a.mergeCancel == nil })
	if !strings.Contains(a.Status, "Staged resolution") {
		t.Fatal(a.Status)
	}
	if unmerged := testutil.Git(t, a.Root, "ls-files", "-u"); unmerged != "" {
		t.Fatal(unmerged)
	}
	if indexed := testutil.Git(t, a.Root, "ls-files", "--", "conflict file.txt"); indexed != "" {
		t.Fatal("deletion not staged", indexed)
	}
}

func TestMergePreviewDoesNotApplyAfterFocusChanges(t *testing.T) {
	a, b := mergeEditor(t)
	original := b.Doc.Text.String()
	chooseMerge(t, a, "Resolve hunk 1")
	chooseMerge(t, a, "Preview ours")
	a.NewBuffer()
	key(a, tcell.KeyEnter)
	if b.Doc.Text.String() != original || a.Preview != nil {
		t.Fatal("preview changed a background buffer", a.Status)
	}
}
