package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
)

func TestRecordingsCLIListExportDelete(t *testing.T) {
	dir := t.TempDir()
	withRecordingPaths(t, config.Paths{StateDir: dir})
	recordings := filepath.Join(dir, "recordings")
	if err := os.MkdirAll(recordings, 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(recordings, "s1.cast")
	if err := os.WriteFile(src, []byte(`{"version":2}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "recordings", "list", "--color", "never")
	if !strings.Contains(out.String(), "s1.cast") {
		t.Fatalf("list = %q", out.String())
	}
	dst := filepath.Join(t.TempDir(), "out.cast")
	out, _ = executeRoot(t, "recordings", "export", "s1", dst, "--color", "never")
	if !strings.Contains(out.String(), "exported") {
		t.Fatalf("export out = %q", out.String())
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"version":2`) {
		t.Fatalf("export body = %q", string(body))
	}
	out, _ = executeRoot(t, "recordings", "delete", "s1", "--color", "never")
	if !strings.Contains(out.String(), "deleted") {
		t.Fatalf("delete out = %q", out.String())
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("recording still exists err=%v", err)
	}
}

func withRecordingPaths(t *testing.T, paths config.Paths) {
	t.Helper()
	old := recordingDefaultPaths
	recordingDefaultPaths = func() (config.Paths, error) { return paths, nil }
	t.Cleanup(func() { recordingDefaultPaths = old })
}
