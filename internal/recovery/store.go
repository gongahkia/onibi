// Package recovery stores private, bounded snapshots without touching originals.
package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gongahkia/shack/internal/text"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Buffer struct {
	Path       string
	Contents   string
	Selections []text.Selection
	BaseHash   string
	Existed    bool
}
type Record struct {
	Schema  int
	Updated time.Time
	Buffers []Buffer
}
type Entry struct {
	Path   string
	Record Record
	lock   *os.File
}
type Store struct {
	dir, path string
	lock      *os.File
	mu        sync.Mutex
	sequence  uint64
	closed    bool
	Limit     int
}

func Directory() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "shack", "recovery")
}
func Open(dir string, limit int) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.CreateTemp(dir, "session-*.lock")
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, err
	}
	return &Store{dir: dir, path: lock.Name() + ".json", lock: lock, Limit: limit}, nil
}
func (s *Store) Save(sequence uint64, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || sequence < s.sequence {
		return nil
	}
	s.sequence = sequence
	total := 0
	for _, b := range record.Buffers {
		total += len(b.Contents)
	}
	if total > s.Limit {
		return errors.New("recovery snapshot exceeds configured limit")
	}
	if len(record.Buffers) == 0 {
		err := os.Remove(s.path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	record.Schema = 1
	record.Updated = time.Now()
	f, err := os.CreateTemp(s.dir, ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = json.NewEncoder(f).Encode(record); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}
func (s *Store) Scan() ([]*Entry, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "session-*.lock.json"))
	if err != nil {
		return nil, err
	}
	out := []*Entry{}
	for _, path := range paths {
		if path == s.path || len(out) >= 20 {
			continue
		}
		lockPath := path[:len(path)-5]
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			continue
		}
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			lock.Close()
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			lock.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(s.Limit)*6+65537))
		f.Close()
		var record Record
		if err != nil || len(data) > s.Limit*6+65536 || json.Unmarshal(data, &record) != nil || record.Schema != 1 || len(record.Buffers) > 64 {
			lock.Close()
			continue
		}
		size := 0
		valid := true
		for _, b := range record.Buffers {
			size += len(b.Contents)
			if b.Path != "" && !filepath.IsAbs(b.Path) {
				valid = false
			}
		}
		if !valid || size > s.Limit {
			lock.Close()
			continue
		}
		out = append(out, &Entry{Path: path, Record: record, lock: lock})
	}
	return out, nil
}
func (e *Entry) Release(discard bool) error {
	var err error
	if discard {
		err = os.Remove(e.Path)
		if err == nil {
			_ = os.Remove(e.lock.Name())
		}
	}
	_ = e.lock.Close()
	return err
}
func (s *Store) Close(discard bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if discard {
		_ = os.Remove(s.path)
	}
	_ = s.lock.Close()
	if discard {
		_ = os.Remove(s.lock.Name())
	}
}
func (e *Entry) Label() string {
	return fmt.Sprintf("%s · %d unsaved buffer(s)", e.Record.Updated.Format("2006-01-02 15:04"), len(e.Record.Buffers))
}
