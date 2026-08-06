package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/store"
)

func TestClaudeQuestionInputAndAnswers(t *testing.T) {
	raw := []byte(`{"questions":[{"header":"Mode","question":"Choose mode","options":[{"label":"Safe"},{"label":"Fast"}]},{"header":"Flags","question":"Choose flags","multiSelect":true,"options":[{"label":"A"},{"label":"B"}]}]}`)
	_, canonical, err := parseClaudeQuestionInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateClaudeAnswers(canonical, map[string]string{"Choose mode": "Safe", "Choose flags": "A,B"}); err != nil {
		t.Fatal(err)
	}
	if err := validateClaudeAnswers(canonical, map[string]string{"Choose mode": "Safe,Fast", "Choose flags": "A"}); err == nil {
		t.Fatal("accepted multiple single-select answers")
	}
}

func TestClaudeQuestionWaiterResumesWithAnswers(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, ClaudeQuestionTimeout: time.Minute})
	s := NewSession("claude-question", "claude", "claude", 4096)
	s.Transport, s.TmuxTarget = "tmux", "onibi-claude-question"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	raw := `{"questions":[{"header":"Mode","question":"Choose mode","options":[{"label":"Safe"}]}]}`
	result := make(chan intake.Response, 1)
	go func() {
		response, _ := d.handleClaudeQuestion(context.Background(), intake.Event{Session: s.ID, Agent: "claude", InputJSON: raw})
		result <- response
	}()
	select {
	case event := <-d.ClaudeQuestionEvents():
		if event.Question == nil {
			t.Fatal("missing question")
		}
		if _, err := d.AnswerClaudeQuestion(context.Background(), event.Question.ID, map[string]string{"Choose mode": "Safe"}, 42); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("question was not emitted")
	}
	select {
	case response := <-result:
		if response.Decision != "approve" || response.Answers["Choose mode"] != "Safe" {
			t.Fatalf("response=%#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("question waiter did not resume")
	}
}

func TestClaudeQuestionStoreState(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	input, _ := json.Marshal(map[string]any{"questions": []map[string]any{{"header": "Mode", "question": "Choose mode", "options": []map[string]string{{"label": "Safe"}}}}})
	item := store.ClaudeQuestion{ID: "question-1", SessionID: "session-1", InputJSON: input, ExpiresAt: time.Now().Add(time.Minute)}
	if err := db.ClaudeQuestionCreate(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	resolved, changed, err := db.ClaudeQuestionResolve(t.Context(), item.ID, store.ClaudeQuestionAnswered, map[string]string{"Choose mode": "Safe"}, "answered", 42)
	if err != nil || !changed || resolved.Answers["Choose mode"] != "Safe" {
		t.Fatalf("resolved=%#v changed=%t err=%v", resolved, changed, err)
	}
	_, changed, err = db.ClaudeQuestionResolve(t.Context(), item.ID, store.ClaudeQuestionCancelled, nil, "late", 42)
	if err != nil || changed {
		t.Fatalf("late changed=%t err=%v", changed, err)
	}
}
