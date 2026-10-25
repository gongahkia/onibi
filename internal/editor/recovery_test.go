package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/gongahkia/shack/internal/recovery"
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryPreservesExternalConflictAndUnreadableOriginal(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(map[bool]string{true: "binary", false: "changed"}[binary], func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "file")
			os.WriteFile(path, []byte("original"), 0600)
			hash := sha256.Sum256([]byte("original"))
			store, err := recovery.Open(recovery.Directory(), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Save(1, recovery.Record{Buffers: []recovery.Buffer{{Path: path, Contents: "recovered", BaseHash: hex.EncodeToString(hash[:]), Existed: true, Selections: []text.Selection{{Anchor: 9, Head: 9}}}}}); err != nil {
				t.Fatal(err)
			}
			store.Close(false)
			contents := "external"
			if binary {
				contents = "\x00binary"
			}
			os.WriteFile(path, []byte(contents), 0600)
			a := testApp(t)
			a.startRecovery()
			defer func() { a.quit = true; a.finishRecovery() }()
			if len(a.recoverable) != 1 {
				t.Fatal(a.Status)
			}
			a.restoreRecovery(a.recoverable[0])
			b := a.Current()
			if b.Doc.Text.String() != "recovered" {
				t.Fatal(a.Status)
			}
			if binary {
				if b.File.Path != "" {
					t.Fatal("binary file should recover as untitled")
				}
			} else if b.File.Save(b.Doc) == nil {
				t.Fatal("overwrote external changes")
			}
			data, _ := os.ReadFile(path)
			if string(data) != contents {
				t.Fatal("original changed")
			}
		})
	}
}
