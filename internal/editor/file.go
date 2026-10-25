package editor

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/gongahkia/shack/internal/text"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type File struct {
	Path   string
	exists bool
	hash   [32]byte
}

func OpenFile(path string, limit int64) (*File, *text.Document, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	f := &File{Path: abs}
	handle, err := os.Open(abs)
	if os.IsNotExist(err) {
		d, _ := text.NewDocument("")
		return f, d, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("only regular text files can be opened")
	}
	if info.Size() > limit {
		return nil, nil, fmt.Errorf("file exceeds configured limit (%d MiB)", limit>>20)
	}
	data, err := io.ReadAll(io.LimitReader(handle, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, errors.New("file grew beyond configured limit")
	}
	if strings.ContainsRune(string(data), 0) {
		return nil, nil, errors.New("binary file: NUL bytes found")
	}
	d, err := text.NewDocument(string(data))
	if err != nil {
		return nil, nil, err
	}
	f.hash = sha256.Sum256(data)
	f.exists = true
	return f, d, nil
}

// Save stages in the same directory, checks for external changes, and preserves
// permissions. Symlinks resolve at open; new destinations are never overwritten.
func (f *File) Save(d *text.Document) error {
	if f.Path == "" {
		return errors.New("choose a path with Save as")
	}
	mode := os.FileMode(0644)
	var owner *syscall.Stat_t
	check := func() error {
		info, err := os.Lstat(f.Path)
		if !f.exists {
			if os.IsNotExist(err) {
				return nil
			}
			if err == nil {
				return errors.New("destination already exists; open it before overwriting")
			}
			return err
		}
		if err != nil {
			return fmt.Errorf("file changed or disappeared: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("destination is no longer a regular file")
		}
		if info.Mode().Perm()&0222 == 0 {
			return errors.New("file is read-only")
		}
		handle, err := os.Open(f.Path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, handle)
		handle.Close()
		if err != nil {
			return err
		}
		if string(h.Sum(nil)) != string(f.hash[:]) {
			return errors.New("file changed on disk; use Save as to preserve your edits")
		}
		mode = info.Mode().Perm()
		owner, _ = info.Sys().(*syscall.Stat_t)
		if owner != nil && owner.Nlink > 1 {
			return errors.New("file has hard links; use Save as to avoid breaking them")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".shack-save-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if owner != nil {
		if err = tmp.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
			return err
		}
	}
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	h := sha256.New()
	if _, err = d.Text.WriteTo(io.MultiWriter(tmp, h)); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	if f.exists {
		err = os.Rename(tmp.Name(), f.Path)
	} else {
		err = os.Link(tmp.Name(), f.Path)
	}
	if err != nil {
		return err
	}
	copy(f.hash[:], h.Sum(nil))
	f.exists = true
	d.MarkSaved()
	if dir, e := os.Open(filepath.Dir(f.Path)); e == nil {
		e = dir.Sync()
		dir.Close()
		if e != nil {
			return fmt.Errorf("saved, but directory sync failed: %w", e)
		}
	}
	return nil
}
