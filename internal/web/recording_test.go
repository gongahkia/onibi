package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
)

func TestCastWriterWritesAsciinemaEvents(t *testing.T) {
	started := time.Unix(100, 0).UTC()
	path := filepath.Join(t.TempDir(), "s1.cast")
	w, err := newCastWriter(path, "s1", 1024, 40, 100, started)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.writePTY(started.Add(100*time.Millisecond), []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.writePTY(started.Add(200*time.Millisecond), pty.ResizeFrame(24, 80)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	lines := readCastLines(t, path)
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header["version"].(float64) != 2 || header["width"].(float64) != 100 || header["height"].(float64) != 40 || header["title"].(string) != "s1" {
		t.Fatalf("header = %#v", header)
	}
	var out []any
	if err := json.Unmarshal([]byte(lines[1]), &out); err != nil {
		t.Fatal(err)
	}
	if out[1] != "o" || out[2] != "hello" {
		t.Fatalf("output event = %#v", out)
	}
	var resize []any
	if err := json.Unmarshal([]byte(lines[2]), &resize); err != nil {
		t.Fatal(err)
	}
	if resize[1] != "r" || resize[2] != "80x24" {
		t.Fatalf("resize event = %#v", resize)
	}
}

func TestCastWriterRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.cast")
	w, err := newCastWriter(path, "s1", 140, 40, 100, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := w.writePTY(time.Unix(100, int64(i+1)), []byte(strings.Repeat("x", 80))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rotated, err := filepath.Glob(filepath.Join(dir, "s1-*.cast"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated) == 0 {
		t.Fatal("expected rotated cast")
	}
	if len(readCastLines(t, path)) == 0 {
		t.Fatal("current cast missing header")
	}
}

func TestRecorderSubscribesLiveOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	host, err := pty.Spawn(ctx, pty.SpawnOptions{Name: "/bin/sh", Args: []string{"-c", "sleep 0.1; printf hi"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecorder(t.TempDir())
	if err := rec.Record(ctx, "s1", host); err != nil {
		t.Fatal(err)
	}
	_ = host.Wait()
	path := rec.Path("s1")
	deadline := time.Now().Add(2 * time.Second)
	for {
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), `"hi"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recording body = %q err=%v", string(body), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRecorderListSummarizesCastFiles(t *testing.T) {
	started := time.Unix(200, 0).UTC()
	rec := NewRecorder(t.TempDir())
	w, err := newCastWriter(rec.Path("s1"), "s1", 1024, 40, 100, started)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.writePTY(started.Add(2500*time.Millisecond), []byte("done")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := rec.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	item := items[0]
	if item.ID != "s1" || item.SessionID != "s1" || item.Name != "s1.cast" {
		t.Fatalf("item = %#v", item)
	}
	if !item.CreatedAt.Equal(started) {
		t.Fatalf("created_at = %s", item.CreatedAt)
	}
	if item.DurationSeconds != 2.5 {
		t.Fatalf("duration = %v", item.DurationSeconds)
	}
}

func readCastLines(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(body)), "\n")
}
