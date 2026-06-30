package pty

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/terminfo"
)

func TestSpawnEchoCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name: "/bin/echo",
		Args: []string{"hello pty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	buf := drainSubscription(t, ch)

	if !bytes.Contains(buf.Bytes(), []byte("hello pty")) {
		t.Fatalf("expected echoed text, got %q", buf.String())
	}
}

func TestSpawnNonexistentBinary(t *testing.T) {
	_, err := Spawn(context.Background(), SpawnOptions{Name: "/definitely/not/a/binary/onibi-test"})
	if err == nil {
		t.Fatal("expected error on missing binary")
	}
}

func TestSpawnArgv0Override(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name:  "/bin/sh",
		Argv0: "-sh",
		Args:  []string{"-c", "printf %s \"$0\""},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	buf := drainSubscription(t, ch)
	if !bytes.Contains(buf.Bytes(), []byte("-sh")) {
		t.Fatalf("expected argv0 override, got %q", buf.String())
	}
}

func TestSpawnEmptyName(t *testing.T) {
	_, err := Spawn(context.Background(), SpawnOptions{Name: ""})
	if err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestAppendTerminalEnvDefaultsFallsBackWhenGhosttyMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	env := appendTerminalEnvDefaults(nil)
	if got := envValue(env, "TERM"); got != fallbackTerm {
		t.Fatalf("TERM = %q, want %q", got, fallbackTerm)
	}
	if got := envValue(env, "COLORTERM"); got != "truecolor" {
		t.Fatalf("COLORTERM = %q, want truecolor", got)
	}
}

func TestAppendTerminalEnvDefaultsUsesGhosttyWhenInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := terminfo.PrimaryInstallPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("compiled"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := appendTerminalEnvDefaults(nil)
	if got := envValue(env, "TERM"); got != terminfo.XtermGhostty {
		t.Fatalf("TERM = %q, want %q", got, terminfo.XtermGhostty)
	}
	if got := envValue(env, "COLORTERM"); got != "truecolor" {
		t.Fatalf("COLORTERM = %q, want truecolor", got)
	}
}

func TestAppendTerminalEnvDefaultsPreservesExplicitEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	env := appendTerminalEnvDefaults([]string{"TERM=screen-256color", "COLORTERM=24bit"})
	if got := envValue(env, "TERM"); got != "screen-256color" {
		t.Fatalf("TERM = %q", got)
	}
	if got := envValue(env, "COLORTERM"); got != "24bit" {
		t.Fatalf("COLORTERM = %q", got)
	}
}

func TestSpawnSetsGhosttyEnvAndTputColors(t *testing.T) {
	if _, err := exec.LookPath("tic"); err != nil {
		t.Skip("tic unavailable")
	}
	if _, err := exec.LookPath("tput"); err != nil {
		t.Skip("tput unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := terminfo.EnsureXtermGhosttyAt(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name: "/bin/sh",
		Args: []string{"-c", `printf 'term=%s\ncolorterm=%s\ncolors=' "$TERM" "$COLORTERM"; tput colors`},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	buf := drainSubscription(t, ch)
	got := buf.String()
	for _, want := range []string{"term=xterm-ghostty", "colorterm=truecolor", "colors=256"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q: %q", want, got)
		}
	}
}

func TestWriteAndRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name: "/bin/cat",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	ctxSub, cancelSub := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelSub()
	_, ch, unsub := h.Subscribe(ctxSub, 0)
	defer unsub()

	// cat echoes stdin to stdout; PTY master is bidirectional
	go func() {
		_, _ = h.Write([]byte("ping\n"))
		time.Sleep(100 * time.Millisecond)
		_ = h.Close() // forces cat to exit
	}()

	var buf bytes.Buffer
	for p := range ch {
		buf.Write(p)
		if bytes.Contains(buf.Bytes(), []byte("ping")) {
			return
		}
	}
	t.Fatalf("expected to see 'ping' in PTY echo, got %q", buf.Bytes())
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func drainSubscription(t *testing.T, ch <-chan []byte) bytes.Buffer {
	t.Helper()
	done := make(chan bytes.Buffer, 1)
	go func() {
		var buf bytes.Buffer
		for p := range ch {
			buf.Write(p)
		}
		done <- buf
	}()
	select {
	case buf := <-done:
		return buf
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for PTY subscription to close")
	}
	return bytes.Buffer{}
}
