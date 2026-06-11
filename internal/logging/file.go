package logging

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultMaxBytes = 1 << 20
	DefaultBackups  = 3
)

func OpenRotating(path string, maxBytes int64, backups int) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("log path required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if backups < 0 {
		backups = DefaultBackups
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() >= maxBytes {
		if err := rotate(path, backups); err != nil {
			return nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func rotate(path string, backups int) error {
	if backups == 0 {
		return os.Remove(path)
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for i := backups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", path, i)
		next := fmt.Sprintf("%s.%d", path, i+1)
		if err := os.Rename(old, next); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(path, path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
