package editor

import (
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func workspaceFixture(t *testing.T) (*App, []*workspaceTarget) {
	t.Helper()
	a := testApp(t)
	a.Root = t.TempDir()
	targets := []*workspaceTarget{}
	for _, name := range []string{"one.txt", "two.txt"} {
		path := filepath.Join(a.Root, name)
		if err := os.WriteFile(path, []byte("one"), 0600); err != nil {
			t.Fatal(err)
		}
		target, err := a.targetForPath(path)
		if err != nil {
			t.Fatal(err)
		}
		target.Edits = []text.Edit{{Start: 0, End: 3, Text: "two"}}
		targets = append(targets, target)
	}
	return a, targets
}
func TestWorkspaceEditsValidateBeforePublishing(t *testing.T) {
	a, targets := workspaceFixture(t)
	targets[1].Edits[0].End = 100
	a.showWorkspacePreview("bad", targets)
	a.applyWorkspacePreview()
	if len(a.Buffers) != 1 || targets[0].Buffer.Doc.Text.String() != "one" {
		t.Fatal("partial mutation")
	}
	targets[1].Edits[0].End = 3
	a.showWorkspacePreview("good", targets)
	a.applyWorkspacePreview()
	if len(a.Buffers) != 3 {
		t.Fatal(a.Status)
	}
	for _, target := range targets {
		if target.Buffer.Doc.Text.String() != "two" {
			t.Fatal(a.Status)
		}
		data, _ := os.ReadFile(target.Buffer.File.Path)
		if string(data) != "one" {
			t.Fatal("wrote disk")
		}
	}
	a.undoWorkspace(false)
	for _, target := range targets {
		if target.Buffer.Doc.Text.String() != "one" {
			t.Fatal("partial undo")
		}
	}
	a.undoWorkspace(true)
	for _, target := range targets {
		if target.Buffer.Doc.Text.String() != "two" {
			t.Fatal("partial redo")
		}
	}
	targets[1].Buffer.Doc.Insert("x")
	a.undoWorkspace(false)
	if targets[0].Buffer.Doc.Text.String() != "two" {
		t.Fatal("unguarded undo")
	}
}
func TestWorkspaceDiskConflictAndCheckbox(t *testing.T) {
	a, targets := workspaceFixture(t)
	a.showWorkspacePreview("replace", targets)
	os.WriteFile(targets[1].Buffer.File.Path, []byte("external"), 0600)
	a.applyWorkspacePreview()
	if len(a.Buffers) != 1 {
		t.Fatal("partial disk-conflicting edit")
	}
	a.Preview.Checked[1] = false
	a.applyWorkspacePreview()
	if len(a.Buffers) != 2 {
		t.Fatal(a.Status)
	}
}

func TestProjectSearchDeduplicatesCanonicalOpenPaths(t *testing.T) {
	a, targets := workspaceFixture(t)
	real := a.Root
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	a.Root = link
	if err := a.Open(targets[0].Buffer.File.Path); err != nil {
		t.Fatal(err)
	}
	a.searchProject("one", "two", true)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for a.Preview == nil {
		select {
		case fn := <-a.updates:
			fn()
		case <-timer.C:
			t.Fatal(a.Status)
		}
	}
	if len(a.Preview.Edits) != 2 {
		t.Fatal("duplicate canonical path", len(a.Preview.Edits))
	}
	a.applyWorkspacePreview()
	if a.Preview != nil {
		t.Fatal(a.Status)
	}
}
