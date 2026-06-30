package terminfo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceIncludesGhosttyCapabilities(t *testing.T) {
	src := Source()
	for _, want := range []string{
		"xterm-ghostty|ghostty|Ghostty",
		"\tTc,",
		"\tSu,",
		"\tsetrgbf=\\E[38:2:%p1%d:%p2%d:%p3%dm,",
		"\tsetrgbb=\\E[48:2:%p1%d:%p2%d:%p3%dm,",
		"\tfullkbd,",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("source missing %q", want)
		}
	}
}

func TestEnsureXtermGhosttySkipsExistingInstall(t *testing.T) {
	home := t.TempDir()
	want := PrimaryInstallPath(home)
	if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("compiled"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldLookPath := lookPath
	lookPath = func(string) (string, error) {
		t.Fatal("tic lookup should be skipped")
		return "", errors.New("unreachable")
	}
	t.Cleanup(func() { lookPath = oldLookPath })
	got, err := EnsureXtermGhosttyAt(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestEnsureXtermGhosttyMissingTIC(t *testing.T) {
	oldLookPath := lookPath
	lookPath = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = oldLookPath })
	_, err := EnsureXtermGhosttyAt(context.Background(), t.TempDir())
	if !errors.Is(err, ErrMissingTIC) {
		t.Fatalf("err = %v, want ErrMissingTIC", err)
	}
	if !strings.Contains(err.Error(), "brew install ncurses") {
		t.Fatalf("err missing remediation: %v", err)
	}
}

func TestEnsureXtermGhosttyRunsTIC(t *testing.T) {
	binDir := t.TempDir()
	tic := filepath.Join(binDir, "tic")
	if err := os.WriteFile(tic, []byte(`#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift
done
if [ -z "$out" ]; then exit 2; fi
mkdir -p "$out/78"
printf compiled > "$out/78/xterm-ghostty"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	oldLookPath := lookPath
	lookPath = func(name string) (string, error) {
		if name != "tic" {
			return "", exec.ErrNotFound
		}
		return tic, nil
	}
	t.Cleanup(func() { lookPath = oldLookPath })
	home := t.TempDir()
	got, err := EnsureXtermGhosttyAt(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	want := PrimaryInstallPath(home)
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("primary path missing: %v", err)
	}
}

func TestEnsureXtermGhosttyInstallsAndInfocmpResolves(t *testing.T) {
	if _, err := exec.LookPath("tic"); err != nil {
		t.Skip("tic unavailable")
	}
	if _, err := exec.LookPath("infocmp"); err != nil {
		t.Skip("infocmp unavailable")
	}
	home := t.TempDir()
	got, err := EnsureXtermGhosttyAt(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	if got != PrimaryInstallPath(home) {
		t.Fatalf("path = %q, want %q", got, PrimaryInstallPath(home))
	}
	outDir := filepath.Join(home, ".terminfo")
	out, err := exec.Command("infocmp", "-x", "-A", outDir, XtermGhostty).CombinedOutput()
	if err != nil {
		t.Fatalf("infocmp failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "xterm-ghostty|ghostty|Ghostty") {
		t.Fatalf("infocmp output missing term name: %s", string(out))
	}
}
