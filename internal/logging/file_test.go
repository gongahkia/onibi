package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRotatingRotatesOversizeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onibi.log")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRotating(path, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %#o", fi.Mode().Perm())
	}
}
