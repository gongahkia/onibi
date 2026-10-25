package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestGitIgnoresAndTrackedIgnoredFiles(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, e := c.CombinedOutput(); e != nil {
			t.Fatalf("%s: %v", out, e)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0600)
	os.WriteFile(filepath.Join(root, "skip.log"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(root, "keep.log"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0600)
	run("add", "-f", "keep.log")
	files, _, err := Files(context.Background(), root)
	if err != nil || slices.Contains(files, "skip.log") || !slices.Contains(files, "keep.log") || !slices.Contains(files, "a.txt") {
		t.Fatal(files, err)
	}
}
func TestFallbackAndBoundary(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "node_modules"), 0700)
	os.WriteFile(filepath.Join(root, "node_modules", "skip"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0600)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "linked"))
	files, _, err := Files(context.Background(), root)
	if err != nil || len(files) != 1 || files[0] != "a.txt" {
		t.Fatal(files, err)
	}
	if Inside(root, filepath.Join(root, "linked")) {
		t.Fatal("symlink escaped")
	}
}
