package recovery

import (
	"os"
	"testing"
)

func TestPrivateRecoveryAndLiveSessionIsolation(t *testing.T) {
	dir := t.TempDir()
	a, e := Open(dir, 1024)
	if e != nil {
		t.Fatal(e)
	}
	record := Record{Buffers: []Buffer{{Contents: "important unsaved text"}}}
	if e = a.Save(2, record); e != nil {
		t.Fatal(e)
	}
	a.Save(1, Record{})
	info, _ := os.Stat(a.path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("recovery is not private")
	}
	b, _ := Open(dir, 1024)
	defer b.Close(true)
	entries, _ := b.Scan()
	if len(entries) != 0 {
		t.Fatal("live session stolen")
	}
	a.Close(false)
	entries, _ = b.Scan()
	if len(entries) != 1 || entries[0].Record.Buffers[0].Contents != "important unsaved text" {
		t.Fatal("recovery lost")
	}
	if e = entries[0].Release(true); e != nil {
		t.Fatal(e)
	}
	entries, _ = b.Scan()
	if len(entries) != 0 {
		t.Fatal("discard failed")
	}
}
