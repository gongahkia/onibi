package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskOutputNavigationExcludesCommandHeader(t *testing.T) {
	a := testApp(t)
	a.Root = t.TempDir()
	path := filepath.Join(a.Root, "source.txt")
	os.WriteFile(path, []byte("first\nsecond"), 0600)
	a.startTask("test", []string{"sh", "-c", "printf 'source.txt:2:1: test message\\n'"})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for a.taskCancel != nil {
		select {
		case fn := <-a.updates:
			fn()
		case <-deadline.C:
			t.Fatal("task did not finish")
		}
	}
	a.outputLocations()
	if a.Dialog == nil || len(a.Dialog.Items) != 1 {
		t.Fatal(a.Status)
	}
	a.Dialog.Items[0].Run()
	if a.Current().File.Path == "" || a.Current().Doc.Selections[0].Head != 6 {
		t.Fatal(a.Current().File.Path, a.Current().Doc.Selections)
	}
}
