package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/timeline"
)

func TestWebTimelineParsesActiveClaudeTranscript(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := budget.ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(base, "projects", key, "abc.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"assistant","message":{"role":"assistant","content":"done","usage":{"input_tokens":3,"output_tokens":4},"model":"claude-sonnet-4-5"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Budget: budget.NewClaudeParser(base)})
	if err := db.SessionUpsertStart(t.Context(), "s1", "main", "claude", cwd, "claude", "pty", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	events, err := d.WebTimeline(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTimelineKind(events, timeline.KindTurn) || !hasTimelineKind(events, timeline.KindCost) {
		t.Fatalf("events = %#v", events)
	}
}

func hasTimelineKind(events []timeline.TimelineEvent, kind timeline.EventKind) bool {
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}
