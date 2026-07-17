package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
)

func TestNewLeavesHistoricalRecordingsUntouched(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "recordings", "legacy.cast")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("legacy recording"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = New(Options{Paths: config.Paths{StateDir: stateDir}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "legacy recording" {
		t.Fatalf("recording changed: %q", data)
	}
}
