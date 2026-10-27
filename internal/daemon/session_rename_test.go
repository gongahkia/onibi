package daemon

import (
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestRenameSessionUpdatesRegistryAndDatabase(t *testing.T) {
	d, _ := testDaemon(t)
	s := addTmuxSession(t, d, "aaaa1111", "build")
	if err := d.DB.SessionUpsertStart(t.Context(), s.ID, s.Name(), s.Agent, "/repo", "shell", "tmux", s.TmuxTarget, time.Now()); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionControl, Session: "build", Action: "rename", Name: " review "})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID != s.ID || resp.Text != "review" || s.Name() != "review" {
		t.Fatalf("resp=%#v name=%q", resp, s.Name())
	}
	row, ok, err := d.DB.Session(t.Context(), s.ID)
	if err != nil || !ok || row.Name != "review" {
		t.Fatalf("row=%#v ok=%v err=%v", row, ok, err)
	}
	if got, err := d.sessionForRPCTarget("review"); err != nil || got != s {
		t.Fatalf("new name does not resolve: %v", err)
	}
	if _, err := d.sessionForRPCTarget("build"); err == nil {
		t.Fatal("old name still resolves")
	}
	if s.TmuxTarget != "onibi-aaaa1111" {
		t.Fatalf("multiplexer target changed to %q", s.TmuxTarget)
	}
}

func TestRenameSessionRejectsInvalidAndTakenNames(t *testing.T) {
	d, _ := testDaemon(t)
	s := addTmuxSession(t, d, "aaaa1111", "build")
	addTmuxSession(t, d, "bbbb2222", "logs")
	for _, name := range []string{"", "  ", "logs", "has space", "semi;colon", string(make([]byte, 65))} {
		_, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionControl, Session: s.ID, Action: "rename", Name: name, Exact: true})
		if err == nil || !intake.IsRejected(err) {
			t.Fatalf("name %q: err=%v", name, err)
		}
	}
	if s.Name() != "build" {
		t.Fatalf("name changed to %q", s.Name())
	}
	resp, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionControl, Session: s.ID, Action: "rename", Name: "build", Exact: true})
	if err != nil || resp.Text != "build" {
		t.Fatalf("renaming to the current name should succeed: %#v %v", resp, err)
	}
}

func TestRenameSessionExactRequiresFullID(t *testing.T) {
	d, _ := testDaemon(t)
	addTmuxSession(t, d, "aaaa1111", "build")
	for _, id := range []string{"aaaa", "build"} {
		if _, err := d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionControl, Session: id, Action: "rename", Name: "x", Exact: true}); err == nil {
			t.Fatalf("exact rename accepted %q", id)
		}
	}
}

func TestRenameSessionIsSafeWithConcurrentReads(t *testing.T) {
	d, _ := testDaemon(t)
	s := addTmuxSession(t, d, "aaaa1111", "build")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = s.Name()
			_, _ = d.handleRPCRequest(t.Context(), intake.Event{Type: intake.TypeSessionList})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			name := "a"
			if i%2 == 0 {
				name = "b"
			}
			_, _ = d.RenameSession(t.Context(), s, name)
		}
	}()
	wg.Wait()
}
