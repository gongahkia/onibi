package fileops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
)

// Validate checks every captured file and listed directory before any mutation.
// This is optimistic conflict detection, not a lock against other processes.
func Validate(ctx context.Context, root string, image map[string]Node) error {
	for path, want := range image {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := checkedPath(root, path); err != nil {
			return err
		}
		got, err := diskNode(path, MaxBytes)
		if err != nil {
			return err
		}
		if !same(got, want) {
			return fmt.Errorf("resource changed on disk: %s", path)
		}
		if want.Kind == Directory && want.Listed {
			d, err := os.Open(path)
			if err != nil {
				return err
			}
			names, err := d.Readdirnames(MaxNodes + 1)
			d.Close()
			if err != nil && err != io.EOF {
				return err
			}
			slices.Sort(names)
			if !slices.Equal(names, want.Children) {
				return fmt.Errorf("directory contents changed: %s", path)
			}
		}
	}
	return nil
}

func metadata(path string, n Node) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if int(stat.Uid) != n.UID || int(stat.Gid) != n.GID {
		if err = os.Chown(path, n.UID, n.GID); err != nil {
			return err
		}
	}
	return os.Chmod(path, n.Mode)
}

func Commit(ctx context.Context, root string, before, after map[string]Node) error {
	return commit(ctx, root, before, after, nil)
}

// fault is a deterministic failure-injection point used by transaction tests.
func commit(ctx context.Context, root string, before, after map[string]Node, fault func(string) error) (err error) {
	if len(before) > MaxNodes || len(after) > MaxNodes {
		return errors.New("workspace operation exceeds 512 paths")
	}
	paths := []string{}
	bytes := 0
	for path, n := range after {
		if _, ok := before[path]; !ok {
			return errors.New("missing original resource snapshot")
		}
		bytes += len(n.Data)
		if bytes > MaxBytes {
			return errors.New("workspace operation exceeds 8 MiB")
		}
		if !same(before[path], n) {
			paths = append(paths, path)
		}
	}
	if len(before) != len(after) {
		return errors.New("incomplete resource snapshot")
	}
	sort.Strings(paths)
	if err = Validate(ctx, root, before); err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	stage, err := os.MkdirTemp(root, ".shack-edit-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	staged := map[string]string{}
	for i, path := range paths {
		if err = ctx.Err(); err != nil {
			return err
		}
		n := after[path]
		if n.Kind != File {
			continue
		}
		name := filepath.Join(stage, fmt.Sprintf("new-%d", i))
		f, e := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(n.Data)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
		if e = metadata(name, n); e != nil {
			return e
		}
		staged[path] = name
	}
	if err = Validate(ctx, root, before); err != nil {
		return err
	}
	undo := []func() error{}
	createdDirectories := map[string]os.FileInfo{}
	step := func(label string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if fault != nil {
			return fault(label)
		}
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		failures := []string{}
		// Final permissions may be read-only. Make only our own new directories
		// writable again so their published children can be rolled back.
		for path, info := range createdDirectories {
			if now, e := os.Lstat(path); e == nil && os.SameFile(info, now) {
				if e = os.Chmod(path, 0700); e != nil {
					failures = append(failures, e.Error())
				}
			}
		}
		for i := len(undo) - 1; i >= 0; i-- {
			if e := undo[i](); e != nil {
				failures = append(failures, e.Error())
			}
		}
		// Restored directories stay writable until their children are restored.
		for _, path := range paths {
			if n := before[path]; n.Kind == Directory {
				if e := metadata(path, n); e != nil {
					failures = append(failures, e.Error())
				}
			}
		}
		if len(failures) > 0 {
			keep = true
			err = fmt.Errorf("%w; rollback incomplete (%s); backups retained in %s", err, strings.Join(failures, "; "), stage)
		}
	}()
	// Move originals to a private same-filesystem staging directory. A backup
	// retains the original inode/metadata for rollback until the whole edit works.
	for i, path := range paths {
		if before[path].Kind != File {
			continue
		}
		if err = step("backup " + path); err != nil {
			return err
		}
		backup := filepath.Join(stage, fmt.Sprintf("old-%d", i))
		if err = os.Rename(path, backup); err != nil {
			return err
		}
		undo = append(undo, func() error {
			if _, e := checkedPath(root, path); e != nil {
				return e
			}
			if e := os.Link(backup, path); e != nil {
				return e
			}
			return os.Remove(backup)
		})
		got, e := diskNode(backup, MaxBytes)
		if e != nil {
			return e
		}
		if !same(got, before[path]) {
			return fmt.Errorf("resource changed while staging: %s", path)
		}
	}
	// Descendants sort after their parents; remove deepest directories first.
	for i := len(paths) - 1; i >= 0; i-- {
		path := paths[i]
		if before[path].Kind != Directory || after[path].Kind == Directory {
			continue
		}
		if err = step("remove directory " + path); err != nil {
			return err
		}
		if err = os.Remove(path); err != nil {
			return err
		}
		undo = append(undo, func() error { return os.Mkdir(path, 0700) })
	}
	for _, path := range paths {
		if after[path].Kind != Directory || before[path].Kind == Directory {
			continue
		}
		if err = step("create directory " + path); err != nil {
			return err
		}
		if _, err = checkedPath(root, path); err != nil {
			return err
		}
		if err = os.Mkdir(path, 0700); err != nil {
			return err
		}
		info, _ := os.Stat(path)
		createdDirectories[path] = info
		undo = append(undo, func() error {
			now, e := os.Lstat(path)
			if e != nil {
				return e
			}
			if !os.SameFile(info, now) {
				return fmt.Errorf("new directory changed: %s", path)
			}
			return os.Remove(path)
		})
	}
	for _, path := range paths {
		name := staged[path]
		if name == "" {
			continue
		}
		if err = step("publish " + path); err != nil {
			return err
		}
		if _, err = checkedPath(root, path); err != nil {
			return err
		}
		// Link is exclusive: a newly appeared destination is never overwritten.
		if err = os.Link(name, path); err != nil {
			return err
		}
		info, _ := os.Stat(path)
		undo = append(undo, func() error {
			now, e := os.Lstat(path)
			if e != nil {
				return e
			}
			if !os.SameFile(info, now) {
				return fmt.Errorf("published file changed: %s", path)
			}
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			data, e := io.ReadAll(io.LimitReader(f, MaxBytes+1))
			f.Close()
			if e != nil {
				return e
			}
			if string(data) != after[path].Data {
				return fmt.Errorf("published file contents changed: %s", path)
			}
			return os.Remove(path)
		})
		if err = os.Remove(name); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if n := after[path]; n.Kind == Directory {
			if err = metadata(path, n); err != nil {
				return err
			}
		}
	}
	if err = step("sync"); err != nil {
		return err
	}
	parents := map[string]bool{root: true}
	for _, path := range paths {
		parent := filepath.Dir(path)
		if after[parent].Kind != Missing || parent == root {
			parents[parent] = true
		}
	}
	for parent := range parents {
		d, e := os.Open(parent)
		if e != nil {
			return e
		}
		e = d.Sync()
		d.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
