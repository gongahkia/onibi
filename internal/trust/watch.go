package trust

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	if ok {
		p = pruneExpired(p, time.Now())
		w.roots[root] = p
	}
	return p, ok
}

func (w *Watcher) AddRuntimeRule(root string, rule Rule) error {
	_, err := w.AddRuntimeRuleWithID(root, rule)
	return err
}

func (w *Watcher) AddRuntimeRuleWithID(root string, rule Rule) (Rule, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Rule{}, err
	}
	root = filepath.Clean(root)
	now := time.Now()
	if rule.Effect == "" {
		rule.Effect = EffectAutoApprove
	}
	if rule.ExpiresRaw == "" && rule.Expires > 0 {
		rule.ExpiresRaw = rule.Expires.String()
	}
	rule.Runtime = true
	if err := rule.validate(-1); err != nil {
		return Rule{}, err
	}
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = NewRuntimeID()
	}
	if !rule.Never && rule.ExpiresAt.IsZero() {
		rule.ExpiresAt = now.Add(rule.Expires)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	p := pruneExpired(w.roots[root], now)
	p.Rules = append([]Rule{rule}, p.Rules...)
	w.roots[root] = p
	return rule, nil
}

func (w *Watcher) View(root string) (View, error) {
	root = strings.TrimSpace(root)
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	var roots []string
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return View{}, err
		}
		roots = []string{filepath.Clean(abs)}
	} else {
		for r := range w.roots {
			roots = append(roots, r)
		}
		sort.Strings(roots)
	}
	view := View{}
	for _, r := range roots {
		p := pruneExpired(w.roots[r], now)
		w.roots[r] = p
		view.Roots = append(view.Roots, ViewForPolicy(r, p, now))
	}
	return view, nil
}

func (w *Watcher) Reload(root string) (WatchEvent, bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return WatchEvent{}, false, err
	}
	root = filepath.Clean(root)
	w.mu.Lock()
	ev, ok := w.loadLocked(root)
	w.mu.Unlock()
	if ok {
		w.emit(ev)
	}
	return ev, ok, nil
}

func (w *Watcher) RemoveRule(root, id string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	root = filepath.Clean(root)
	id = strings.TrimSpace(id)
	if id == "" {
		return false, errors.New("rule id required")
	}
	if strings.HasPrefix(id, "file:") {
		return w.removeFileRule(root, id)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	p := pruneExpired(w.roots[root], time.Now())
	out := p
	out.Rules = out.Rules[:0]
	removed := false
	for _, rule := range p.Rules {
		if rule.Runtime && rule.ID == id {
			removed = true
			continue
		}
		out.Rules = append(out.Rules, rule)
	}
	w.roots[root] = out
	return removed, nil
}

func (w *Watcher) PersistRuntimeRules(root string) (int, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	root = filepath.Clean(root)
	path := PolicyPath(root)
	now := time.Now()
	w.mu.Lock()
	prev := pruneExpired(w.roots[root], now)
	var runtimeRules []Rule
	persisted := map[string]bool{}
	for _, rule := range prev.Rules {
		if rule.Runtime && !rule.expired(now) {
			runtimeRules = append(runtimeRules, PersistedRuleAt(rule, now))
			persisted[rule.ID] = true
		}
	}
	w.mu.Unlock()
	if len(runtimeRules) == 0 {
		return 0, nil
	}
	disk, err := Load(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		disk = Policy{}
	}
	disk = rebaseFileExpiries(removeExpiredFileRules(disk, now), now)
	disk.Rules = append(disk.Rules, runtimeRules...)
	if err := Save(path, disk); err != nil {
		return 0, err
	}
	w.mu.Lock()
	current := pruneExpired(w.roots[root], now)
	noRuntime := current
	noRuntime.Rules = noRuntime.Rules[:0]
	for _, rule := range current.Rules {
		if rule.Runtime && persisted[rule.ID] {
			continue
		}
		noRuntime.Rules = append(noRuntime.Rules, rule)
	}
	w.roots[root] = noRuntime
	ev, ok := w.loadLocked(root)
	w.mu.Unlock()
	if ok {
		w.emit(ev)
	}
	return len(runtimeRules), nil
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
	path := PolicyPath(root)
	prev := w.roots[root]
	runtimeRules := activeRuntimeRules(prev, time.Now())
	p, err := Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			p = Policy{}
			p.Rules = append(runtimeRules, p.Rules...)
			w.roots[root] = p
			return WatchEvent{Root: root, Path: path, Previous: prev, Policy: p}, true
		}
		return WatchEvent{Root: root, Path: path, Previous: prev, Policy: prev, Err: err}, true
	}
	p.Rules = append(runtimeRules, p.Rules...)
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

func (w *Watcher) removeFileRule(root, id string) (bool, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "file:"))
	if err != nil || n <= 0 {
		return false, errors.New("invalid file rule id")
	}
	path := PolicyPath(root)
	disk, err := Load(path)
	if err != nil {
		return false, err
	}
	if n > len(disk.Rules) {
		return false, nil
	}
	disk.Rules = append(disk.Rules[:n-1], disk.Rules[n:]...)
	if err := Save(path, disk); err != nil {
		return false, err
	}
	w.mu.Lock()
	ev, ok := w.loadLocked(root)
	w.mu.Unlock()
	if ok {
		w.emit(ev)
	}
	return true, nil
}

func activeRuntimeRules(p Policy, now time.Time) []Rule {
	var out []Rule
	for _, rule := range p.Rules {
		if rule.Runtime && !rule.expired(now) {
			out = append(out, rule)
		}
	}
	return out
}

func pruneExpired(p Policy, now time.Time) Policy {
	out := p
	out.Rules = out.Rules[:0]
	for _, rule := range p.Rules {
		if !rule.Runtime || !rule.expired(now) {
			out.Rules = append(out.Rules, rule)
		}
	}
	return out
}

func removeExpiredFileRules(p Policy, now time.Time) Policy {
	out := p
	out.Rules = out.Rules[:0]
	for _, rule := range p.Rules {
		if !rule.Runtime && rule.expired(now) {
			continue
		}
		out.Rules = append(out.Rules, rule)
	}
	return out
}

func rebaseFileExpiries(p Policy, now time.Time) Policy {
	for i := range p.Rules {
		rule := &p.Rules[i]
		if rule.Runtime || rule.Never || rule.ExpiresAt.IsZero() {
			continue
		}
		remaining := rule.ExpiresAt.Sub(now)
		if remaining > 0 {
			rule.ExpiresRaw = remaining.String()
			rule.Expires = remaining
		}
		rule.ExpiresAt = time.Time{}
	}
	return p
}
