package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func TestHealthPersistsSessionState(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := newHealthTracker(db, nil)
	ctx := context.Background()
	s := NewSession("s-persist", "work", "shell", 4096)
	s.Transport = "tmux"
	h.tmuxResult(ctx, s, false, errors.New("offline"))
	h2 := newHealthTracker(db, nil)
	h2.load(ctx)
	if got := h2.snapshot().Sessions[s.ID]; got.Status != "unavailable" || got.Error != "offline" {
		t.Fatalf("state=%#v", got)
	}
}

func TestStatusReportsTmuxHealth(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, Paths: config.Paths{StateDir: t.TempDir()}})
	s := NewSession("s-health", "work", "shell", 4096)
	s.TmuxTarget = "onibi-s-health"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.health.tmuxResult(context.Background(), s, true, nil)
	status := d.pingText(context.Background())
	for _, want := range []string{"sessions=1", "multiplexers:", "work (tmux)=healthy"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %s", want, status)
		}
	}
}

func TestHealthNamesActualMultiplexerAndExcludesCodex(t *testing.T) {
	d := New(Options{Paths: config.Paths{StateDir: t.TempDir()}})
	ctx := t.Context()
	for _, backend := range []string{"tmux", "zellij", "screen", "codex"} {
		s := NewSession("health-"+backend, "work-"+backend, "shell", 4096)
		s.Transport = backend
		if err := d.Registry.Add(s); err != nil {
			t.Fatal(err)
		}
		if backend == "codex" {
			continue
		}
		d.health.tmuxResult(ctx, s, true, nil)
		event := d.health.tmuxResult(ctx, s, false, errors.New("backend unavailable"))
		if event == nil || !strings.Contains(event.Text, backend+" health check failed") {
			t.Fatalf("%s failure alert=%#v", backend, event)
		}
		event = d.health.tmuxResult(ctx, s, true, nil)
		if event == nil || !strings.Contains(event.Text, backend+" session work-"+backend+" recovered") {
			t.Fatalf("%s recovery alert=%#v", backend, event)
		}
	}
	status := d.pingText(ctx)
	for _, want := range []string{"codex_sessions=1", "work-tmux (tmux)=healthy", "work-zellij (zellij)=healthy", "work-screen (screen)=healthy"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %s", want, status)
		}
	}
	if strings.Contains(status, "work-codex") {
		t.Fatalf("Codex was incorrectly listed as a multiplexer: %s", status)
	}
}
