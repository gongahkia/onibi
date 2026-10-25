package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Plan {
	t.Helper()
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(p.Root, "old"), 0750); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"old/a": "α😀\r\n", "old/b": "\x00\xffbinary", "replace": "before"} {
		if err = os.WriteFile(filepath.Join(p.Root, path), []byte(body), 0640); err != nil {
			t.Fatal(err)
		}
	}
	return p
}
func mixedPlan(t *testing.T, p *Plan) {
	t.Helper()
	if _, err := p.Rename(filepath.Join(p.Root, "old"), filepath.Join(p.Root, "nested/new"), false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Delete(filepath.Join(p.Root, "nested/new/b"), false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create(filepath.Join(p.Root, "replace"), true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create(filepath.Join(p.Root, "nested/new/c"), false, false); err != nil {
		t.Fatal(err)
	}
}
func TestOrderedResourcesAndUndoRedo(t *testing.T) {
	p := fixture(t)
	mixedPlan(t, p)
	before, after := p.Images()
	if err := Commit(context.Background(), p.Root, before, after); err != nil {
		t.Fatal(err)
	}
	if err := Validate(context.Background(), p.Root, after); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(p.Root, "nested/new/a"))
	if err != nil || string(data) != "α😀\r\n" {
		t.Fatal(string(data), err)
	}
	if err = Commit(context.Background(), p.Root, after, before); err != nil {
		t.Fatal("undo", err)
	}
	if err = Validate(context.Background(), p.Root, before); err != nil {
		t.Fatal(err)
	}
	if err = Commit(context.Background(), p.Root, before, after); err != nil {
		t.Fatal("redo", err)
	}
	entries, _ := filepath.Glob(filepath.Join(p.Root, ".shack-edit-*"))
	if len(entries) > 0 {
		t.Fatal("staging leaked", entries)
	}
}
func TestRollbackEveryCommitStep(t *testing.T) {
	for failure := 1; failure <= 14; failure++ {
		t.Run(string(rune('a'+failure)), func(t *testing.T) {
			p := fixture(t)
			mixedPlan(t, p)
			before, after := p.Images()
			step := 0
			err := commit(context.Background(), p.Root, before, after, func(string) error {
				step++
				if step == failure {
					return errors.New("injected I/O failure")
				}
				return nil
			})
			if step >= failure {
				if err == nil {
					t.Fatal("missing failure")
				}
				if err = Validate(context.Background(), p.Root, before); err != nil {
					t.Fatal("partial change", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			entries, _ := filepath.Glob(filepath.Join(p.Root, ".shack-edit-*"))
			if len(entries) > 0 {
				t.Fatal("backups leaked after successful rollback", entries)
			}
		})
	}
}
func TestOptionsAndConflicts(t *testing.T) {
	p := fixture(t)
	file := filepath.Join(p.Root, "replace")
	if changed, err := p.Create(file, false, true); err != nil || changed {
		t.Fatal("ignore existing", changed, err)
	}
	if changed, err := p.Create(file, true, true); err != nil || !changed {
		t.Fatal("overwrite takes precedence", changed, err)
	}
	if _, err := p.Delete(filepath.Join(p.Root, "old"), false, false); err == nil {
		t.Fatal("nonrecursive directory deletion")
	}
	if changed, err := p.Delete(filepath.Join(p.Root, "absent"), false, true); err != nil || changed {
		t.Fatal(changed, err)
	}
	before, after := p.Images()
	os.WriteFile(file, []byte("external"), 0640)
	if err := Commit(context.Background(), p.Root, before, after); err == nil {
		t.Fatal("overwrote external edit")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "external" {
		t.Fatal("changed disk despite conflict")
	}
	p = fixture(t)
	mixedPlan(t, p)
	before, after = p.Images()
	os.WriteFile(filepath.Join(p.Root, "old/added"), []byte("external"), 0600)
	if err := Commit(context.Background(), p.Root, before, after); err == nil {
		t.Fatal("deleted an unexpected directory entry")
	}
}
func TestRenameOverwriteAndDirectoryRecreation(t *testing.T) {
	p := fixture(t)
	if changed, err := p.Rename(filepath.Join(p.Root, "old/a"), filepath.Join(p.Root, "replace"), false, true); err != nil || changed {
		t.Fatal(changed, err)
	}
	if _, err := p.Rename(filepath.Join(p.Root, "old/a"), filepath.Join(p.Root, "replace"), true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Delete(filepath.Join(p.Root, "old"), true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create(filepath.Join(p.Root, "old/new"), false, false); err != nil {
		t.Fatal(err)
	}
	before, after := p.Images()
	if err := Commit(context.Background(), p.Root, before, after); err != nil {
		t.Fatal(err)
	}
	if err := Validate(context.Background(), p.Root, after); err != nil {
		t.Fatal(err)
	}
	if err := Commit(context.Background(), p.Root, after, before); err != nil {
		t.Fatal(err)
	}
}
func TestPathAndRetentionBounds(t *testing.T) {
	p := fixture(t)
	if _, err := p.Create(filepath.Join(p.Root, "../outside"), false, false); err == nil {
		t.Fatal("escape")
	}
	if _, err := p.Delete(p.Root, true, false); err == nil {
		t.Fatal("root deletion")
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(p.Root, "link"))
	if _, err := p.Create(filepath.Join(p.Root, "link/file"), false, false); err == nil {
		t.Fatal("symlink parent")
	}
	os.Link(filepath.Join(p.Root, "replace"), filepath.Join(p.Root, "hardlink"))
	if _, err := p.Delete(filepath.Join(p.Root, "hardlink"), false, false); err == nil {
		t.Fatal("hardlink")
	}
	if _, err := p.Rename(filepath.Join(p.Root, "old"), filepath.Join(p.Root, "old/sub"), false, false); err == nil {
		t.Fatal("recursive rename")
	}
	os.WriteFile(filepath.Join(p.Root, "large"), []byte(strings.Repeat("x", MaxBytes+1)), 0600)
	if _, err := p.Delete(filepath.Join(p.Root, "large"), false, false); err == nil {
		t.Fatal("unbounded snapshot")
	}
	p, _ = New(t.TempDir())
	for i := 0; i < MaxNodes; i++ {
		if _, err := p.Create(filepath.Join(p.Root, string(rune(0x1000+i))), false, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Create(filepath.Join(p.Root, "too-many"), false, false); err == nil {
		t.Fatal("unbounded paths")
	}
}
func TestCancelledCommitAndIncompleteRollbackPreservesBackup(t *testing.T) {
	p := fixture(t)
	mixedPlan(t, p)
	before, after := p.Images()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Commit(ctx, p.Root, before, after); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p = fixture(t)
	file := filepath.Join(p.Root, "replace")
	p.Create(file, true, false)
	before, after = p.Images()
	err := commit(context.Background(), p.Root, before, after, func(label string) error {
		if label == "sync" {
			os.WriteFile(file, []byte("external"), 0640)
			return errors.New("failure after concurrent writer")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "backups retained") {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "external" {
		t.Fatal("rollback overwrote another writer")
	}
	backups, _ := filepath.Glob(filepath.Join(p.Root, ".shack-edit-*", "old-*"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	data, _ = os.ReadFile(backups[0])
	if string(data) != "before" {
		t.Fatal("lost rollback original")
	}
}

func TestFileToParentDirectoryAndReadOnlyMovedDirectory(t *testing.T) {
	p := fixture(t)
	path := filepath.Join(p.Root, "replace")
	if _, err := p.Delete(path, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create(filepath.Join(path, "child"), false, false); err != nil {
		t.Fatal(err)
	}
	before, after := p.Images()
	if err := Commit(context.Background(), p.Root, before, after); err != nil {
		t.Fatal(err)
	}
	if err := Commit(context.Background(), p.Root, after, before); err != nil {
		t.Fatal(err)
	}
	p = fixture(t)
	if _, err := p.Rename(filepath.Join(p.Root, "old"), filepath.Join(p.Root, "moved"), false, false); err != nil {
		t.Fatal(err)
	}
	before, after = p.Images()
	n := after[filepath.Join(p.Root, "moved")]
	n.Mode = 0500
	after[filepath.Join(p.Root, "moved")] = n
	err := commit(context.Background(), p.Root, before, after, func(label string) error {
		if label == "sync" {
			return errors.New("fail after permissions")
		}
		return nil
	})
	if err == nil {
		t.Fatal("missing error")
	}
	if err = Validate(context.Background(), p.Root, before); err != nil {
		t.Fatal("read-only rollback", err)
	}
}
