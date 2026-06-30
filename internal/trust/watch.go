package trust

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounce = 150 * time.Millisecond

type WatchEvent struct {
	Root     string
	Path     string
	Policy   Policy
	Previous Policy
	Err      error
	Initial  bool
}

type Watcher struct {
	watcher  *fsnotify.Watcher
	debounce time.Duration
	onEvent  func(WatchEvent)

	mu      sync.Mutex
	roots   map[string]Policy
	watched map[string]string
	timers  map[string]*time.Timer
	closed  bool
}

func NewWatcher(onEvent func(WatchEvent)) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		watcher:  w,
		debounce: defaultDebounce,
		onEvent:  onEvent,
		roots:    map[string]Policy{},
		watched:  map[string]string{},
		timers:   map[string]*time.Timer{},
	}, nil
}

func (w *Watcher) AddRoot(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	var events []WatchEvent
	w.mu.Lock()
	if _, ok := w.roots[root]; ok {
		w.mu.Unlock()
		return nil
	}
	w.roots[root] = Policy{}
	events = append(events, w.addWatchLocked(root, root)...)
	onibiDir := filepath.Join(root, ".onibi")
	if isDir(onibiDir) {
		events = append(events, w.addWatchLocked(onibiDir, root)...)
	}
	if ev, ok := w.loadLocked(root); ok {
		ev.Initial = true
		events = append(events, ev)
	}
	w.mu.Unlock()
	w.emit(events...)
	return nil
}

func (w *Watcher) Policy(root string) (Policy, bool) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Policy{}, false
	}
	root = filepath.Clean(root)
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok := w.roots[root]
	return p, ok
}

func (w *Watcher) Run(ctx context.Context) {
	defer w.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleFSEvent(ev)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.emit(WatchEvent{Err: err})
		}
	}
}

func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	return w.watcher.Close()
}

func (w *Watcher) handleFSEvent(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
		return
	}
	var events []WatchEvent
	w.mu.Lock()
	root, ok := w.watched[filepath.Clean(ev.Name)]
	if !ok {
		root = w.rootForPathLocked(ev.Name)
	}
	if root == "" {
		w.mu.Unlock()
		return
	}
	if filepath.Base(ev.Name) == ".onibi" && isDir(ev.Name) {
		events = append(events, w.addWatchLocked(ev.Name, root)...)
	}
	if filepath.Base(ev.Name) == "trust.toml" {
		w.scheduleLocked(root)
	}
	w.mu.Unlock()
	w.emit(events...)
}

func (w *Watcher) scheduleLocked(root string) {
	if t := w.timers[root]; t != nil {
		t.Stop()
	}
	w.timers[root] = time.AfterFunc(w.debounce, func() {
		var ev WatchEvent
		var ok bool
		w.mu.Lock()
		ev, ok = w.loadLocked(root)
		w.mu.Unlock()
		if ok {
			w.emit(ev)
		}
	})
}

func (w *Watcher) loadLocked(root string) (WatchEvent, bool) {
	path := filepath.Join(root, ".onibi", "trust.toml")
	prev := w.roots[root]
	p, err := Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			p = Policy{}
			w.roots[root] = p
			return WatchEvent{Root: root, Path: path, Previous: prev, Policy: p}, true
		}
		return WatchEvent{Root: root, Path: path, Previous: prev, Policy: prev, Err: err}, true
	}
	w.roots[root] = p
	return WatchEvent{Root: root, Path: path, Previous: prev, Policy: p}, true
}

func (w *Watcher) addWatchLocked(path, root string) []WatchEvent {
	path = filepath.Clean(path)
	if _, ok := w.watched[path]; ok {
		return nil
	}
	if err := w.watcher.Add(path); err != nil {
		return []WatchEvent{{Root: root, Path: path, Err: err}}
	}
	w.watched[path] = root
	return nil
}

func (w *Watcher) rootForPathLocked(path string) string {
	path = filepath.Clean(path)
	for watched, root := range w.watched {
		if filepath.Dir(path) == watched || path == filepath.Join(root, ".onibi") {
			return root
		}
	}
	return ""
}

func (w *Watcher) emit(events ...WatchEvent) {
	if w.onEvent == nil {
		return
	}
	for _, ev := range events {
		w.onEvent(ev)
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
