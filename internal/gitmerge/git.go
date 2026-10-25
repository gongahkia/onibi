package gitmerge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

type Blob struct{ Mode, OID string }
type Entry struct {
	Path   string
	Stages [4]Blob
}
type Repository struct{ Root, Scope string }
type bounded struct {
	bytes.Buffer
	Limit    int
	Overflow bool
	Cancel   context.CancelFunc
}

func (b *bounded) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, max(0, b.Limit-b.Len()))
	b.Buffer.Write(p[:keep])
	b.Overflow = b.Overflow || keep < n
	if keep < n && b.Cancel != nil {
		b.Cancel()
	}
	return n, nil
}

func run(ctx context.Context, dir string, limit int, args ...string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "--literal-pathspecs", "-C", dir}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	out, stderr := &bounded{Limit: limit, Cancel: cancel}, &bounded{Limit: 4096}
	cmd.Stdout = out
	cmd.Stderr = stderr
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	err := cmd.Run()
	if out.Overflow {
		return "", errors.New("Git result exceeds the configured inspection bound")
	}
	if err != nil {
		return "", fmt.Errorf("Git: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

func Open(ctx context.Context, scope string) (Repository, error) {
	scope, err := filepath.Abs(scope)
	if err != nil {
		return Repository{}, err
	}
	scope, err = filepath.EvalSymlinks(scope)
	if err != nil {
		return Repository{}, err
	}
	root, err := run(ctx, scope, 4096, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repository{}, err
	}
	root = strings.TrimSuffix(root, "\n")
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Repository{}, err
	}
	return Repository{Root: root, Scope: scope}, nil
}

func (r Repository) Path(entry Entry) (string, error) {
	return r.path(entry, true)
}
func (r Repository) path(entry Entry, checkWorktree bool) (string, error) {
	if entry.Path == "" || filepath.IsAbs(entry.Path) || filepath.Clean(entry.Path) != entry.Path {
		return "", errors.New("invalid Git conflict path")
	}
	path := filepath.Join(r.Root, entry.Path)
	rel, err := filepath.Rel(r.Scope, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("conflict path is outside the open project")
	}
	for at := path; checkWorktree && at != r.Root; at = filepath.Dir(at) {
		info, e := os.Lstat(at)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("conflict path crosses a symlink")
		}
	}
	return path, nil
}

func (r Repository) Conflicts(ctx context.Context) ([]Entry, error) {
	raw, err := run(ctx, r.Scope, 1<<20, "ls-files", "--unmerged", "--full-name", "-z", "--", ".")
	if err != nil {
		return nil, err
	}
	entries := map[string]*Entry{}
	for _, record := range strings.Split(raw, "\x00") {
		if record == "" {
			continue
		}
		header, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || len(path) > 4096 || !utf8.ValidString(path) {
			return nil, errors.New("invalid or non-UTF-8 Git conflict record")
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil || stage < 1 || stage > 3 {
			return nil, errors.New("invalid Git index stage")
		}
		if len(fields[1]) != 40 && len(fields[1]) != 64 {
			return nil, errors.New("invalid Git object ID")
		}
		for _, c := range fields[1] {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return nil, errors.New("invalid Git object ID")
			}
		}
		e := entries[path]
		if e == nil {
			e = &Entry{Path: path}
			if _, err = r.path(*e, false); err != nil {
				return nil, err
			}
			entries[path] = e
		}
		if len(entries) > MaxConflicts {
			return nil, errors.New("more than 2,000 conflicted paths")
		}
		e.Stages[stage] = Blob{fields[0], fields[1]}
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (r Repository) Current(ctx context.Context, entry Entry) error {
	entries, err := r.Conflicts(ctx)
	if err != nil {
		return err
	}
	for _, current := range entries {
		if current.Path == entry.Path {
			if current == entry {
				return nil
			}
			return errors.New("Git conflict stages changed; refresh conflicts")
		}
	}
	return errors.New("file is no longer unmerged; refresh conflicts")
}

func (r Repository) Read(ctx context.Context, entry Entry, stage int) (string, error) {
	if stage < 1 || stage > 3 {
		return "", errors.New("invalid merge stage")
	}
	blob := entry.Stages[stage]
	if blob.OID == "" {
		return "", errors.New("this stage is absent (the file did not exist on that side)")
	}
	if blob.Mode != "100644" && blob.Mode != "100755" {
		return "", errors.New("symlink/submodule conflicts need their respective external tools")
	}
	size, err := run(ctx, r.Root, 128, "cat-file", "-s", blob.OID)
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(strings.TrimSpace(size))
	if err != nil || n < 0 || n > MaxBytes {
		return "", errors.New("merge stage exceeds 8 MiB")
	}
	source, err := run(ctx, r.Root, MaxBytes, "cat-file", "blob", blob.OID)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(source) || strings.IndexByte(source, 0) >= 0 {
		return "", errors.New("binary/encoded conflict: text resolution is unavailable")
	}
	return source, nil
}

func (r Repository) Stage(ctx context.Context, entry Entry) error {
	if err := r.Current(ctx, entry); err != nil {
		return err
	}
	path, err := r.Path(entry)
	if err != nil {
		return err
	}
	_, err = run(ctx, r.Root, 4096, "add", "--", path)
	return err
}
