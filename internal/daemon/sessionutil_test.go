package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeSessionCWD(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := normalizeSessionCWD(dir)
	if err != nil || got != want {
		t.Fatalf("cwd=%q err=%v", got, err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeSessionCWD(file); err == nil {
		t.Fatal("file accepted as working directory")
	}
	if _, err := normalizeSessionCWD(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestSessionNameRejectsTerminalControlCharacters(t *testing.T) {
	d := New(Options{})
	if _, err := d.sessionName("work\nnext", "shell"); err == nil {
		t.Fatal("control character accepted")
	}
	if got, err := d.sessionName("work-1", "shell"); err != nil || got != "work-1" {
		t.Fatalf("name=%q err=%v", got, err)
	}
}
