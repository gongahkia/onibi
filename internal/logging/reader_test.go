package logging

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadLogsTailFollowAndRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var tail []string
	if err := ReadLogs(t.Context(), path, 2, false, func(b []byte) error { tail = append(tail, string(b)); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(tail, ",") != "two,three" {
		t.Fatalf("tail=%v", tail)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	lines := make(chan string, 10)
	done := make(chan error, 1)
	go func() { done <- ReadLogs(ctx, path, 1, true, func(b []byte) error { lines <- string(b); return nil }) }()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("got=%s want=%s", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("follow timeout")
		}
	}
	expect("three")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("four\n")
	f.Close()
	expect("four")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("five\n"), 0600); err != nil {
		t.Fatal(err)
	}
	expect("five")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop reader")
	}
}
func TestReadLogsRejectsLinksAndInvalidLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.log")
	if err := os.Symlink(filepath.Join(dir, "other"), path); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{-1, 1001, 50} {
		if err := ReadLogs(t.Context(), path, n, false, func([]byte) error { return nil }); err == nil {
			t.Fatal("unsafe read accepted")
		}
	}
}
func TestFormatRecordSanitizesConsole(t *testing.T) {
	got := FormatRecord([]byte(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"web.started","value":"\u001b[2J\nother"}`))
	if strings.ContainsAny(got, "\x1b\n") {
		t.Fatalf("unsafe console output %q", got)
	}
	if FormatRecord([]byte("not JSON")) != "[unreadable log record]" {
		t.Fatal("invalid record printed raw")
	}
}
