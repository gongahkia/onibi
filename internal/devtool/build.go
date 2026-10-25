// Package devtool implements the source-checkout build and binary lifecycle.
// It deliberately has no dependency on Ghostty, Telegram, or their runtimes.
package devtool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/termui"
	"golang.org/x/sys/unix"
)

type Tool struct {
	Root, Prefix string
	UI           *termui.UI
	Runner       Runner
	Verbose      bool
}

func (t *Tool) prepare() error {
	if t.Runner == nil {
		t.Runner = ExecRunner{}
	}
	if t.UI == nil {
		t.UI = termui.New(os.Stderr)
	}
	root, err := filepath.Abs(t.Root)
	if err != nil {
		return err
	}
	t.Root = root
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.HasPrefix(string(raw), "module github.com/gongahkia/onibi\n") {
		return errors.New("run this command from the Onibi source checkout")
	}
	for _, name := range []string{"GOOS", "GOARCH"} {
		want := runtime.GOOS
		if name == "GOARCH" {
			want = runtime.GOARCH
		}
		if value := os.Getenv(name); value != "" && value != want {
			return fmt.Errorf("unset %s for a local Onibi build (currently %s)", name, value)
		}
	}
	return nil
}
func (t *Tool) capture(ctx context.Context, name string, args ...string) (string, error) {
	return t.Runner.Run(ctx, t.Root, nil, name, args, nil)
}
func (t *Tool) revision(ctx context.Context) (version, commit string) {
	version, err := t.capture(ctx, "git", "describe", "--tags", "--always", "--dirty")
	if err != nil {
		version = "dev"
	}
	commit, err = t.capture(ctx, "git", "rev-parse", "--short=12", "HEAD")
	if err != nil {
		commit = "unknown"
	}
	return strings.TrimSpace(version), strings.TrimSpace(commit)
}
func privateLog(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func (t *Tool) Build(ctx context.Context) error { return t.build(ctx, nil) }

// afterPublish runs under the build lock so install copies this exact build.
func (t *Tool) build(ctx context.Context, afterPublish func(version, commit string) error) error {
	if err := t.prepare(); err != nil {
		return err
	}
	t.UI.Banner("BUILD / native Ghostty cockpit")
	version, commit := t.revision(ctx)
	t.UI.Field("Revision", version+" / "+commit)
	if runtime.GOOS == "darwin" {
		t.UI.Field("Capture", "CGO enabled for macOS Accessibility")
	}
	output := filepath.Join(t.Root, "bin")
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	lock, err := lockDirectory(output)
	if err != nil {
		return err
	}
	defer lock.Close()
	stage, err := os.MkdirTemp(output, ".build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	logPath := filepath.Join(output, "build.log")
	log, err := privateLog(logPath)
	if err != nil {
		return err
	}
	defer log.Close()
	t.UI.Field("Build log", logPath)
	flags := "-s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=" + version + " -X github.com/gongahkia/onibi/internal/buildinfo.Commit=" + commit + " -X github.com/gongahkia/onibi/internal/buildinfo.Date=" + time.Now().UTC().Format(time.RFC3339)
	env := []string{}
	if runtime.GOOS == "darwin" {
		env = append(env, "CGO_ENABLED=1")
	}
	steps := []struct{ name, pkg, label string }{{"onibi", "./cmd/onibi", "[1/4] Compile native cockpit"}, {"onibi-notify", "./clients/onibi-notify", "[2/4] Compile session notifier"}}
	for _, step := range steps {
		err = t.UI.Step(step.label, func(update func(string)) error {
			_, err := t.Runner.Run(ctx, t.Root, env, "go", []string{"build", "-v", "-trimpath", "-ldflags", flags, "-o", filepath.Join(stage, step.name), step.pkg}, func(line string) {
				fmt.Fprintln(log, line)
				if t.Verbose {
					t.UI.Line("  %s", termui.Clean(line))
				} else {
					update(line)
				}
			})
			return err
		})
		if err != nil {
			t.failure(logPath, log)
			return fmt.Errorf("build stopped; existing binaries were preserved: %w", err)
		}
	}
	if err = t.UI.Step("[3/4] Verify staged executable", func(update func(string)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, name := range binaryNames {
			info, err := os.Stat(filepath.Join(stage, name))
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() == 0 || info.Mode()&0111 == 0 {
				return fmt.Errorf("invalid staged executable: %s", name)
			}
		}
		text, err := t.Runner.Run(ctx, t.Root, nil, filepath.Join(stage, "onibi"), []string{"version"}, nil)
		update(strings.TrimSpace(text))
		return err
	}); err != nil {
		return fmt.Errorf("staged binary verification failed; existing binaries were preserved: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = t.UI.Step("[4/4] Publish local binaries", func(func(string)) error { return promote(stage, output, binaryNames) }); err != nil {
		return err
	}
	if afterPublish != nil {
		return afterPublish(version, commit)
	}
	t.UI.Note("Ready: ./bin/onibi web")
	t.UI.Note("Fresh link: ./bin/onibi web pair  |  Diagnostics: ./bin/onibi web doctor")
	t.UI.Note("An already-running Onibi process keeps its current version until you restart it.")
	return nil
}
func (t *Tool) failure(path string, file *os.File) {
	_ = file.Sync()
	raw, err := os.ReadFile(path)
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) > 18 {
			lines = lines[len(lines)-18:]
		}
		for _, line := range lines {
			t.UI.Note(line)
		}
	}
	t.UI.Field("Full output", path)
}

var binaryNames = []string{"onibi", "onibi-notify"}

func lockDirectory(dir string) (*os.File, error) {
	fd, err := unix.Open(filepath.Join(dir, ".onibi-lifecycle.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "lifecycle lock")
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another Onibi build or lifecycle operation is using this directory")
	}
	return f, nil
}
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Each destination replacement is atomic. If a later replacement fails, restore
// previously replaced files. A process already executing an old inode continues.
func promote(stage, destination string, names []string) error {
	backup, err := os.MkdirTemp(destination, ".rollback-")
	if err != nil {
		return err
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			_ = os.RemoveAll(backup)
		}
	}()
	existed := map[string]bool{}
	for _, name := range names {
		path := filepath.Join(destination, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file %s", path)
		}
		existed[name] = true
		if err := copyExecutable(path, filepath.Join(backup, name)); err != nil {
			return err
		}
	}
	var replaced []string
	for _, name := range names {
		if err := os.Rename(filepath.Join(stage, name), filepath.Join(destination, name)); err != nil {
			var rollbackErr error
			for _, old := range replaced {
				path := filepath.Join(destination, old)
				if existed[old] {
					rollbackErr = errors.Join(rollbackErr, os.Rename(filepath.Join(backup, old), path))
				} else {
					rollbackErr = errors.Join(rollbackErr, os.Remove(path))
				}
			}
			if rollbackErr != nil {
				keepBackup = true
				rollbackErr = fmt.Errorf("rollback incomplete; recovery copies retained at %s: %w", backup, rollbackErr)
			}
			return errors.Join(fmt.Errorf("replace %s: %w", name, err), rollbackErr)
		}
		replaced = append(replaced, name)
	}
	return nil
}
