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

func TestHealthPersistsPollAlertsAndRecovery(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := newHealthTracker(db, nil)
	ctx := context.Background()
	for range 2 {
		if event := h.pollResult(ctx, errors.New("offline")); event != nil {
			t.Fatalf("early alert=%#v", event)
		}
	}
	if event := h.pollResult(ctx, errors.New("offline")); event == nil || !strings.Contains(event.Text, "three") {
		t.Fatalf("alert=%#v", event)
	}
	if event := h.pollResult(ctx, nil); event == nil || !strings.Contains(event.Text, "recovered") {
		t.Fatalf("recovery=%#v", event)
	}
	h2 := newHealthTracker(db, nil)
	h2.load(ctx)
	if got := h2.snapshot(); got.PollSuccess.IsZero() || got.PollFailures != 0 {
		t.Fatalf("state=%#v", got)
	}
}

func TestStatusReportsQueueAndTmuxHealth(t *testing.T) {
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
	if err := db.TelegramOutboxUpsert(context.Background(), store.TelegramOutboxIntent{ID: "health-outbox", DedupeKey: "health-outbox", Kind: "notice", ChatID: 1, Title: "queued"}); err != nil {
		t.Fatal(err)
	}
	status := d.pingText(context.Background())
	for _, want := range []string{"poll=never", "outbox=pending:1", "tmux:", "work=healthy"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %s", want, status)
		}
	}
}
