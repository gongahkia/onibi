package editor

import (
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConflictPermissionsAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	os.WriteFile(path, []byte("old\r\n"), 0600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	f, d, err := OpenFile(link, 1024)
	if err != nil {
		t.Fatal(err)
	}
	d.Selections = []text.Selection{{Anchor: 0, Head: 3}}
	d.Insert("new")
	if err = f.Save(d); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new\r\n" {
		t.Fatal(string(data))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions changed")
	}
	info, _ = os.Lstat(link)
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	d.Insert("!")
	os.WriteFile(path, []byte("external"), 0600)
	if err = f.Save(d); err == nil {
		t.Fatal("external edits overwritten")
	}
	data, _ = os.ReadFile(path)
	if string(data) != "external" {
		t.Fatal("disk corrupted")
	}
}
func TestNewFileNeverClobbersAndInvalidFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new")
	f, d, err := OpenFile(p, 1024)
	if err != nil {
		t.Fatal(err)
	}
	d.Insert("ours")
	os.WriteFile(p, []byte("theirs"), 0644)
	if f.Save(d) == nil {
		t.Fatal("new destination clobbered")
	}
	os.WriteFile(p, []byte{0xff}, 0644)
	if _, _, err = OpenFile(p, 1024); err == nil {
		t.Fatal("invalid UTF8")
	}
	os.WriteFile(p, []byte("abc"), 0644)
	if _, _, err = OpenFile(p, 2); err == nil {
		t.Fatal("size limit")
	}
}
