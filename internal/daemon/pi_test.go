package daemon

import (
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/store"
)

func TestPiLifecyclePublishesOnlyForManagedPiSession(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, Paths: config.Paths{Socket: filepath.Join(t.TempDir(), "onibi.sock")}})
	s := NewSession("pi-1", "pi", "pi", 4096)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleAgentLifecycle(t.Context(), intake.Event{Session: s.ID, Agent: "pi", Lifecycle: "agent_start", RunID: "run-1"})
	if err != nil || resp.Text != "agent_start" {
		t.Fatalf("response=%#v err=%v", resp, err)
	}
	select {
	case event := <-d.AgentEvents():
		if event.SessionID != s.ID || event.Agent != "pi" || event.Kind != "agent_start" || event.RunID != "run-1" {
			t.Fatalf("event=%#v", event)
		}
	default:
		t.Fatal("event not published")
	}
	if _, err := d.handleAgentLifecycle(t.Context(), intake.Event{Session: s.ID, Agent: "shell", Lifecycle: "agent_end", RunID: "run-1"}); err == nil {
		t.Fatal("accepted non-Pi lifecycle")
	}
}

func TestClaudeLifecyclePublishesManagedSessionEnd(t *testing.T) {
	d := New(Options{Paths: config.Paths{Socket: filepath.Join(t.TempDir(), "onibi.sock")}})
	s := NewSession("claude-1", "claude", "claude", 4096)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleAgentLifecycle(t.Context(), intake.Event{Session: s.ID, Agent: "claude", Lifecycle: "agent_end"})
	if err != nil || resp.Text != "agent_end" {
		t.Fatalf("response=%#v err=%v", resp, err)
	}
	if event := <-d.AgentEvents(); event.Agent != "claude" || event.Kind != "agent_end" {
		t.Fatalf("event=%#v", event)
	}
	if _, err := d.handleAgentLifecycle(t.Context(), intake.Event{Session: s.ID, Agent: "claude", Lifecycle: "agent_failed"}); err != nil {
		t.Fatal(err)
	}
	if event := <-d.AgentEvents(); event.Agent != "claude" || event.Kind != "agent_failed" {
		t.Fatalf("event=%#v", event)
	}
}
