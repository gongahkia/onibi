package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/faulttest"
	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestFaultProcessExitEndsOnceAndPreservesApproval(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "fault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := New(Options{DB: db})
	process := faulttest.NewProcess()
	session := NewSession("session-fault-exit", "Claude", "claude", pty.NewVirtualHost(process.Write, process.Close, process.Wait), 0)
	if err := d.Registry.Add(session); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(t.Context(), session.ID, session.Name, session.Agent, "/tmp", "claude", "pty", "", session.StartedAt()); err != nil {
		t.Fatal(err)
	}
	approvalID, _, err := d.Queue.Request(t.Context(), session.ID, session.Agent, "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		d.waitHost(session)
		close(done)
	}()
	select {
	case <-process.WaitStarted():
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	process.Exit(errors.New("fault injected process exit"))
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	d.waitHost(session)
	entry, found, err := db.Session(t.Context(), session.ID)
	if err != nil || !found || !entry.Ended {
		t.Fatalf("session=%#v found=%v err=%v", entry, found, err)
	}
	pending, err := d.Queue.Get(t.Context(), approvalID)
	if err != nil || pending.State != approval.StatePending {
		t.Fatalf("approval=%#v err=%v", pending, err)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var ended int
	for _, entry := range audit {
		if entry.Action == "session.end" && entry.SessionID == session.ID {
			ended++
		}
	}
	if ended != 1 {
		t.Fatalf("session end audit count=%d entries=%#v", ended, audit)
	}
}

func TestFaultDelayedTmuxDisappearanceMarksRecoveryWithoutDuplicate(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "fault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	if err := db.SessionUpsertStart(t.Context(), "session-fault-tmux", "Claude", "claude", "/tmp", "claude", "tmux", "onibi-fault", now); err != nil {
		t.Fatal(err)
	}
	gate := faulttest.NewGate()
	runner := &faulttest.Runner{Gate: gate, RunFunc: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "list-sessions" {
			return nil, nil
		}
		return nil, errors.New("unexpected tmux command")
	}}
	previous := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = previous })
	d := New(Options{DB: db})
	done := make(chan struct{})
	go func() {
		d.restoreSessions(t.Context())
		close(done)
	}()
	select {
	case <-gate.Started():
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if entry, found, err := db.Session(t.Context(), "session-fault-tmux"); err != nil || !found || entry.RecoveryState != fleet.SessionRecoveryHealthy {
		t.Fatalf("delayed entry=%#v found=%v err=%v", entry, found, err)
	}
	gate.Release()
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	entry, found, err := db.Session(t.Context(), "session-fault-tmux")
	if err != nil || !found || entry.RecoveryState != fleet.SessionRecoveryOrphaned {
		t.Fatalf("entry=%#v found=%v err=%v", entry, found, err)
	}
	if sessions := d.Registry.List(); len(sessions) != 0 {
		t.Fatalf("duplicate sessions=%#v", sessions)
	}
	calls := runner.Calls()
	if len(calls) != 1 || len(calls[0].Args) == 0 || calls[0].Args[0] != "list-sessions" {
		t.Fatalf("tmux calls=%#v", calls)
	}
}
