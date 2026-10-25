package logging

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// RotatingWriter bounds an ongoing process's logs, not just logs at startup.
// One Onibi web process owns it under the existing exclusive web-state lock.
type RotatingWriter struct {
	mu      sync.Mutex
	path    string
	max     int64
	backups int
	file    *os.File
	size    int64
	closed  bool
}

func NewRotatingWriter(path string, maxBytes int64, backups int) (*RotatingWriter, error) {
	if path == "" {
		return nil, errors.New("log path required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if backups < 0 {
		backups = DefaultBackups
	}
	w := &RotatingWriter{path: path, max: maxBytes, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *RotatingWriter) open() error {
	if info, err := os.Lstat(w.path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular log file: %s", w.path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	fd, err := unix.Open(w.path, unix.O_CREAT|unix.O_APPEND|unix.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	w.file = os.NewFile(uintptr(fd), w.path)
	if err := w.file.Chmod(0600); err != nil {
		w.file.Close()
		return err
	}
	info, err := w.file.Stat()
	if err != nil {
		w.file.Close()
		return err
	}
	w.size = info.Size()
	return nil
}
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if int64(len(p)) > w.max {
		return 0, errors.New("log record exceeds rotation limit")
	}
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > w.max {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		w.file = nil
		if err := rotate(w.path, w.backups); err != nil {
			return 0, err
		}
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}
