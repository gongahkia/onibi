package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSensitivePayloadsPurgeKeepsPendingAndScrubsResolved(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour).Unix()
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO approvals(id,session_id,agent,tool,input_json,state,created_at,decided_at,expires_at) VALUES
		('done','s','pi','tool','{"secret":"yes"}','approved',?,?,?),
		('pending','s','pi','tool','{"secret":"wait"}','pending',?,NULL,?)`, old, old, old, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO claude_questions(id,session_id,input_json,state,answers_json,created_at,decided_at,expires_at) VALUES
		('question','s','{"secret":"yes"}','answered','{"answer":"yes"}',?,?,?)`, old, old, old); err != nil {
		t.Fatal(err)
	}
	if err := db.SensitivePayloadsPurge(ctx, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"done": "{}", "pending": `{"secret":"wait"}`} {
		var got string
		if err := db.SQL().QueryRowContext(ctx, `SELECT input_json FROM approvals WHERE id=?`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("approval %s: got=%q err=%v", id, got, err)
		}
	}
	var input string
	var answers any
	if err := db.SQL().QueryRowContext(ctx, `SELECT input_json,answers_json FROM claude_questions WHERE id='question'`).Scan(&input, &answers); err != nil || input != "{}" || answers != nil {
		t.Fatalf("question: input=%q answers=%v err=%v", input, answers, err)
	}
}
