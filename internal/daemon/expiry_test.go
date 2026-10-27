package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
)

func TestRemoveRetiredUploadsDeletesStagedFiles(t *testing.T) {
	d, _ := testDaemon(t)
	staged := filepath.Join(d.Paths.StateDir, "uploads", "session-1")
	if err := os.MkdirAll(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "photo.jpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.removeRetiredUploads()
	if _, err := os.Stat(filepath.Join(d.Paths.StateDir, "uploads")); !os.IsNotExist(err) {
		t.Fatalf("retired uploads dir remains: %v", err)
	}
	if _, err := os.Stat(d.Paths.Config); err != nil {
		t.Fatalf("removed unrelated state: %v", err)
	}
}

func TestRemoveRetiredUploadsNeedsStateDir(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Mkdir("uploads", 0o700); err != nil {
		t.Fatal(err)
	}
	d := New(Options{Paths: config.Paths{}})
	d.removeRetiredUploads()
	if _, err := os.Stat(filepath.Join(cwd, "uploads")); err != nil {
		t.Fatalf("removed a relative uploads dir: %v", err)
	}
}
