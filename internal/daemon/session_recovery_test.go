package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestRestoreSessionsRecoversTmuxWithoutDuplicates(t *testing.T) {
	db, err := store.OpenEphemeral(t.TempDir() + "/recovery.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.SessionUpsertStart(ctx, "session-123", "Claude", "claude", "/tmp", "claude", "tmux", "onibi-session", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	runner := &sessionRecoveryTmuxRunner{output: []byte("restored\n")}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	d.restoreSessions(ctx)
	entry, ok, err := db.Session(ctx, "session-123")
	if err != nil || !ok || entry.RecoveryState != fleet.SessionRecoveryHealthy || entry.RecoveryReason != "" {
		t.Fatalf("restored session=%#v ok=%v err=%v", entry, ok, err)
	}
	if _, err := d.Registry.Get("session-123"); err != nil {
		t.Fatal(err)
	}
	d.restoreSessions(ctx)
	if sessions := d.Registry.List(); len(sessions) != 1 || sessions[0].ID != "session-123" {
		t.Fatalf("restored sessions=%#v", sessions)
	}
}

func TestRestoreSessionsOrphansMissingTmuxWithoutCancellingApproval(t *testing.T) {
	db, err := store.OpenEphemeral(t.TempDir() + "/recovery.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SessionUpsertStart(ctx, "session-123", "Claude", "claude", "/tmp", "claude", "tmux", "onibi-session", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	runner := &sessionRecoveryTmuxRunner{err: errors.New("no tmux server")}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	approvalID, _, err := d.Queue.Request(ctx, "session-123", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	d.restoreSessions(ctx)
	entry, ok, err := db.Session(ctx, "session-123")
	if err != nil || !ok || entry.Ended || entry.RecoveryState != fleet.SessionRecoveryOrphaned || entry.RecoveryReason == "" {
		t.Fatalf("orphaned session=%#v ok=%v err=%v", entry, ok, err)
	}
	pending, err := d.Queue.Get(ctx, approvalID)
	if err != nil || pending.State != approval.StatePending {
		t.Fatalf("pending approval=%#v err=%v", pending, err)
	}
}

func TestTmuxCaptureDisconnectTimesOutAsOrphaned(t *testing.T) {
	db, err := store.OpenEphemeral(t.TempDir() + "/recovery.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.SessionUpsertStart(ctx, "session-123", "Claude", "claude", "/tmp", "claude", "tmux", "onibi-session", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db})
	d.tmuxCaptureInterval = time.Millisecond
	d.tmuxRecoveryTimeout = 5 * time.Millisecond
	s := NewSession("session-123", "Claude", "claude", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-session"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.captureTmuxLoop(ctx, tmux.NewWithRunner(&sessionRecoveryTmuxRunner{err: errors.New("connection lost")}), s)
	entry, ok, err := db.Session(ctx, "session-123")
	if err != nil || !ok || entry.Ended || entry.RecoveryState != fleet.SessionRecoveryOrphaned || entry.RecoveryReason == "" || s.Ended() {
		t.Fatalf("timeout session=%#v ok=%v err=%v ended=%v", entry, ok, err, s.Ended())
	}
}

type sessionRecoveryTmuxRunner struct {
	output []byte
	err    error
}

func (r *sessionRecoveryTmuxRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return r.output, r.err
}
