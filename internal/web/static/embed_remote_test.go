//go:build onibi_remote

package static

import (
	"io/fs"
	"testing"
)

func TestRemoteArchiveContainsCockpit(t *testing.T) {
	if _, err := fs.ReadFile(FS, "dist/index.html"); err != nil {
		t.Fatal(err)
	}
	assets, err := fs.Sub(FS, "dist/assets")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	if err := fs.WalkDir(assets, ".", func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("archive has no assets")
	}
	fonts, err := fs.Sub(FS, "fonts")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	if err := fs.WalkDir(fonts, ".", func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("archive has no fonts")
	}
}
