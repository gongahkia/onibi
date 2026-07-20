package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
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
	runner := &sessionRecoveryTmuxRunner{listOutput: []byte("onibi-session\n"), sessionIDs: map[string]string{"onibi-session": "session-123"}, output: []byte("restored\n")}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	d.restoreSessions(ctx)
	entry, ok, err := db.Session(ctx, "session-123")
	if err != nil || !ok || entry.RecoveryState != store.SessionRecoveryHealthy || entry.RecoveryReason != "" {
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
	if err != nil || !ok || entry.Ended || entry.RecoveryState != store.SessionRecoveryOrphaned || entry.RecoveryReason == "" {
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
	if err != nil || !ok || entry.Ended || entry.RecoveryState != store.SessionRecoveryOrphaned || entry.RecoveryReason == "" || s.Ended() {
		t.Fatalf("timeout session=%#v ok=%v err=%v ended=%v", entry, ok, err, s.Ended())
	}
}

func TestRestoreSessionsReconcilesDiscoveredTmuxOwnership(t *testing.T) {
	db, err := store.OpenEphemeral(t.TempDir() + "/recovery.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now().UTC()
	for _, row := range []struct {
		id     string
		target string
	}{
		{"session-live", "onibi-live"},
		{"session-duplicate", "onibi-live"},
		{"session-missing", "onibi-missing"},
		{"session-foreign", "onibi-foreign"},
	} {
		if err := db.SessionUpsertStart(ctx, row.id, row.id, "claude", "/tmp", "claude", "tmux", row.target, now); err != nil {
			t.Fatal(err)
		}
	}
	runner := &sessionRecoveryTmuxRunner{listOutput: []byte("onibi-live\nonibi-unowned\nonibi-foreign\n"), sessionIDs: map[string]string{"onibi-live": "session-live", "onibi-foreign": "other-session"}, output: []byte("restored\n")}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	d.restoreSessions(ctx)
	for _, tc := range []struct {
		id    string
		state store.SessionRecoveryState
	}{
		{"session-live", store.SessionRecoveryHealthy},
		{"session-duplicate", store.SessionRecoveryFailed},
		{"session-missing", store.SessionRecoveryOrphaned},
		{"session-foreign", store.SessionRecoveryOrphaned},
	} {
		entry, ok, err := db.Session(ctx, tc.id)
		if err != nil || !ok || entry.RecoveryState != tc.state || entry.Ended {
			t.Fatalf("session %q = %#v ok=%v err=%v", tc.id, entry, ok, err)
		}
	}
	if sessions := d.Registry.List(); len(sessions) != 1 || sessions[0].ID != "session-live" {
		t.Fatalf("reconciled sessions=%#v", sessions)
	}
	if runner.count("kill-session") != 0 || runner.count("capture-pane") != 1 {
		t.Fatalf("tmux calls=%#v", runner.callsSnapshot())
	}
}

func TestRestoreSessionsRetainsOwnershipWhenDiscoveryFails(t *testing.T) {
	db, err := store.OpenEphemeral(t.TempDir() + "/recovery.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SessionUpsertStart(ctx, "session-123", "Claude", "claude", "/tmp", "claude", "tmux", "onibi-session", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	runner := &sessionRecoveryTmuxRunner{listErr: errors.New("tmux unavailable")}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	d.restoreSessions(ctx)
	entry, ok, err := db.Session(ctx, "session-123")
	if err != nil || !ok || entry.Ended || entry.RecoveryState != store.SessionRecoveryReconnecting || entry.RecoveryReason == "" {
		t.Fatalf("discovery failure session=%#v ok=%v err=%v", entry, ok, err)
	}
	if sessions := d.Registry.List(); len(sessions) != 0 || runner.count("capture-pane") != 0 || runner.count("kill-session") != 0 {
		t.Fatalf("discovery failure sessions=%#v calls=%#v", sessions, runner.callsSnapshot())
	}
}

type sessionRecoveryTmuxRunner struct {
	mu         sync.Mutex
	listOutput []byte
	listErr    error
	sessionIDs map[string]string
	output     []byte
	err        error
	calls      [][]string
}

func (r *sessionRecoveryTmuxRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) > 0 && args[0] == "list-sessions" {
		return r.listOutput, r.listErr
	}
	if len(args) == 4 && args[0] == "show-environment" {
		if identity, ok := r.sessionIDs[args[2]]; ok {
			return []byte(args[3] + "=" + identity + "\n"), nil
		}
		return nil, nil
	}
	return r.output, r.err
}

func (r *sessionRecoveryTmuxRunner) count(command string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, call := range r.calls {
		if len(call) > 1 && call[1] == command {
			n++
		}
	}
	return n
}

func (r *sessionRecoveryTmuxRunner) callsSnapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}
