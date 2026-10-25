package editor

import (
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionRestoresFilesPanesAndCursors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := testApp(t)
	a.Root = t.TempDir()
	path := filepath.Join(a.Root, "a.txt")
	os.WriteFile(path, []byte("abcdef"), 0600)
	a.Open(path)
	a.Current().Doc.Selections = []text.Selection{{Anchor: 4, Head: 4}}
	a.draw()
	a.split(false)
	a.Current().Doc.Selections = []text.Selection{{Anchor: 1, Head: 1}}
	if err := a.saveSession(); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.Root = a.Root
	b.restoreSession()
	if len(b.Buffers) != 1 || len(b.panes) != 2 || b.Current().Doc.Selections[0].Head != 1 {
		t.Fatal(b.Status, b.panes)
	}
	b.focusPane(0)
	if b.Current().Doc.Selections[0].Head != 4 {
		t.Fatal(b.Current().Doc.Selections)
	}
	st, _ := os.Stat(a.sessionPath())
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}
func TestSessionDoesNotReplaceDirtyOpenBuffer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := testApp(t)
	a.Root = t.TempDir()
	path := filepath.Join(a.Root, "a")
	os.WriteFile(path, []byte("disk"), 0600)
	a.Open(path)
	a.saveSession()
	a.insert("unsaved")
	before := a.Current().Doc.Text.String()
	a.restoreSession()
	if a.Current().Doc.Text.String() != before {
		t.Fatal("replaced unsaved buffer")
	}
}
