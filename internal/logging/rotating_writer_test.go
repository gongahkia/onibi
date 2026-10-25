package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestContinuousLogRotationIsBoundedPrivateAndParseable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.log")
	w, err := NewRotatingWriter(path, 1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	logger := Web(w, nil, slog.LevelInfo)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				logger.Info("web.request", "status", 200)
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 1024 || info.Mode().Perm() != 0600 {
			t.Fatalf("unbounded or public log: %s", name)
		}
		raw, _ := os.ReadFile(name)
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if !json.Valid([]byte(line)) {
				t.Fatalf("corrupt log line %s", line)
			}
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatal("too many backups")
	}
	if _, err := w.Write([]byte("later")); err == nil {
		t.Fatal("write after close")
	}
}
func TestLogWriterRefusesSymlinkAndConsoleIsPlain(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keep")
	os.WriteFile(target, []byte("preserve"), 0600)
	path := filepath.Join(dir, "web.log")
	os.Symlink(target, path)
	if w, err := NewRotatingWriter(path, 1024, 1); err == nil {
		w.Close()
		t.Fatal("followed log symlink")
	}
	var file, console bytes.Buffer
	logger := Web(&file, &console, slog.LevelInfo)
	logger.Debug("hidden")
	logger.Info("web.native_state", "state", "ready")
	if !json.Valid(bytes.TrimSpace(file.Bytes())) || !strings.Contains(console.String(), "native state") || strings.Contains(console.String(), "\x1b") {
		t.Fatal(fmt.Sprint(&file, &console))
	}
}
