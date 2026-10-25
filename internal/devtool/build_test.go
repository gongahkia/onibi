package devtool

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/termui"
)

type fakeRunner struct {
	failNotifier bool
	contents     string
	calls        [][]string
}

func (f *fakeRunner) Run(_ context.Context, dir string, env []string, name string, args []string, line func(string)) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if name == "git" {
		return "test-revision\n", nil
	}
	if name == "go" {
		if f.failNotifier && args[len(args)-1] == "./clients/onibi-notify" {
			if line != nil {
				line("compiler: deliberate failure")
			}
			return "", errors.New("build failed")
		}
		for i, a := range args {
			if a == "-o" {
				if err := os.WriteFile(args[i+1], []byte(func() string {
					if f.contents != "" {
						return f.contents
					}
					return "new binary"
				}()), 0755); err != nil {
					return "", err
				}
			}
		}
		if line != nil {
			line("github.com/gongahkia/onibi/internal/web")
		}
		return "", nil
	}
	return "v-test\n", nil
}
func sourceTool(t *testing.T) (*Tool, *fakeRunner, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/gongahkia/onibi\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	out := &bytes.Buffer{}
	return &Tool{Root: root, Runner: r, UI: termui.New(out)}, r, out
}
func TestBuildStagesBothBinariesAndPreservesOldOnFailure(t *testing.T) {
	tool, r, out := sourceTool(t)
	bin := filepath.Join(tool.Root, "bin")
	os.MkdirAll(bin, 0755)
	for _, name := range binaryNames {
		os.WriteFile(filepath.Join(bin, name), []byte("old binary"), 0755)
	}
	r.failNotifier = true
	if err := tool.Build(t.Context()); err == nil {
		t.Fatal("ignored compiler failure")
	}
	for _, name := range binaryNames {
		raw, _ := os.ReadFile(filepath.Join(bin, name))
		if string(raw) != "old binary" {
			t.Fatal("failed build replaced working binary")
		}
	}
	if !strings.Contains(out.String(), "deliberate failure") || strings.Contains(out.String(), "\x1b") {
		t.Fatal("missing/plain compiler failure")
	}
	r.failNotifier = false
	if err := tool.Build(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range binaryNames {
		raw, _ := os.ReadFile(filepath.Join(bin, name))
		if string(raw) != "new binary" {
			t.Fatal("binary not promoted")
		}
	}
	info, _ := os.Stat(filepath.Join(bin, "build.log"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("build log not private")
	}
}
func TestPromotionRollsBackAndRefusesSymlinks(t *testing.T) {
	dest := t.TempDir()
	stage := t.TempDir()
	os.WriteFile(filepath.Join(dest, "onibi"), []byte("old"), 0755)
	os.WriteFile(filepath.Join(stage, "onibi"), []byte("new"), 0755)
	if err := promote(stage, dest, binaryNames); err == nil {
		t.Fatal("missing staged file accepted")
	}
	raw, _ := os.ReadFile(filepath.Join(dest, "onibi"))
	if string(raw) != "old" {
		t.Fatal("rollback lost old binary")
	}
	os.Remove(filepath.Join(dest, "onibi"))
	target := filepath.Join(dest, "other")
	os.WriteFile(target, []byte("preserve"), 0755)
	os.Symlink(target, filepath.Join(dest, "onibi"))
	if err := promote(stage, dest, []string{"onibi"}); err == nil {
		t.Fatal("replaced symlink")
	}
	raw, _ = os.ReadFile(target)
	if string(raw) != "preserve" {
		t.Fatal("modified symlink target")
	}
}
func TestBuildLockAndCancellation(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockDirectory(dir); err == nil {
		second.Close()
		t.Fatal("concurrent lifecycle lock accepted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = (ExecRunner{}).Run(ctx, dir, nil, "sh", []string{"-c", "sleep 30 & wait"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation failed: %v", err)
	}
}

func TestBuildKeepsLockThroughInstallationCopy(t *testing.T) {
	tool, _, _ := sourceTool(t)
	called := false
	err := tool.build(t.Context(), func(version, commit string) error {
		called = true
		lock, err := lockDirectory(filepath.Join(tool.Root, "bin"))
		if err == nil {
			lock.Close()
			t.Fatal("another build could replace artifacts during installation")
		}
		if version == "" || commit == "" {
			t.Fatal("lost build provenance")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("installation callback missing")
	}
}
