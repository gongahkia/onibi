package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveBlockTextOnlyRemovesCompleteManagedBlock(t *testing.T) {
	src := "keep\n" + begin + "\nhook\n" + end + "\n" + begin + "\nhook2\n" + end + "\nkeep2\n"
	got, ok := removeBlockText(src)
	if !ok || got != "keep\nkeep2\n" {
		t.Fatalf("got=%q ok=%v", got, ok)
	}
	incomplete, ok := removeBlockText("keep\n" + begin + "\nhook\n")
	if ok || incomplete != "keep\n"+begin+"\nhook\n" {
		t.Fatalf("incomplete=%q ok=%v", incomplete, ok)
	}
}

func TestCleanupLegacyOnlyRemovesMarkedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	zshrc := filepath.Join(home, ".zshrc")
	bashrc := filepath.Join(home, ".bashrc")
	fish := filepath.Join(home, ".config", "fish", "conf.d", "onibi.fish")
	if err := os.WriteFile(zshrc, []byte("before\n"+begin+"\nhook\n"+end+"\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bashrc, []byte("user config\n"+begin+"\nincomplete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fish), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fish, []byte(begin+"\nhook\n"+end+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := CleanupLegacy()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d", removed)
	}
	if got, err := os.ReadFile(zshrc); err != nil || string(got) != "before\nafter\n" {
		t.Fatalf("zshrc = %q err=%v", got, err)
	}
	if got, err := os.ReadFile(bashrc); err != nil || string(got) != "user config\n"+begin+"\nincomplete\n" {
		t.Fatalf("bashrc = %q err=%v", got, err)
	}
	if _, err := os.Stat(fish); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fish hook = %v", err)
	}
}
