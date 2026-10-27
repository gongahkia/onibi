package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

type fakeTmuxRunner struct {
	mu      sync.Mutex
	calls   [][]string
	err     error
	capture func(context.Context) ([]byte, error)
	alerts  string
}

func (r *fakeTmuxRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.mu.Unlock()
	if len(args) > 0 && args[0] == "capture-pane" {
		if r.capture != nil {
			return r.capture(ctx)
		}
		return []byte("ready\n"), r.err
	}
	if len(args) > 0 && args[0] == "display-message" && r.alerts != "" {
		r.mu.Lock()
		defer r.mu.Unlock()
		return []byte(r.alerts), r.err
	}
	return nil, r.err
}

func (r *fakeTmuxRunner) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	for i := range r.calls {
		out[i] = append([]string(nil), r.calls[i]...)
	}
	return out
}

func testDaemon(t *testing.T) (*Daemon, *fakeTmuxRunner) {
	t.Helper()
	state := t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state, Socket: filepath.Join(state, "onibi.sock"), Config: filepath.Join(state, "config.yaml")}, OutputBufferSize: 4096})
	// Use an explicit empty path so mock calls cannot load the host's tmux config.
	cfg := config.Default()
	cfg.Multiplexer.Tmux.Config = ""
	if err := config.Save(d.Paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &fakeTmuxRunner{}
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = oldController })
	return d, runner
}
