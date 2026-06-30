package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/web"
)

func TestDaemonPublishesClaudeCostEvent(t *testing.T) {
	db := openDaemonTestDB(t)
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := budget.ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	providerSession := "claude-session"
	transcript := filepath.Join(base, "projects", key, providerSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(`{"message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":12,"output_tokens":4}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Budget: budget.NewClaudeParser(base)})
	events, unsub := d.Events.Subscribe()
	defer unsub()
	s := NewSession("s1", "claude", "claude", nil, 0)
	s.CWD = cwd
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.handleEvent(t.Context(), intake.Event{
		Type:              intake.TypeAgentDone,
		Session:           "s1",
		Managed:           true,
		Agent:             "claude",
		CWD:               cwd,
		ProviderSessionID: providerSession,
	}); err != nil {
		t.Fatal(err)
	}
	ev := readCostWebEvent(t, events)
	cost, ok := ev.Payload.(budget.CostEvent)
	if !ok {
		t.Fatalf("payload = %#v", ev.Payload)
	}
	if cost.SessionID != "s1" || cost.ProviderSessionID != providerSession || cost.InputTokens != 12 || cost.OutputTokens != 4 || cost.Model != "claude-sonnet-4-5" {
		t.Fatalf("cost = %#v", cost)
	}
}

func readCostWebEvent(t *testing.T, events <-chan web.Event) web.Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("cost event not delivered")
		return web.Event{}
	}
}
