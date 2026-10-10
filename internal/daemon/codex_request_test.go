package daemon

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCodexRequestCannotBeAnsweredWithConflictingDecision(t *testing.T) {
	d := New(Options{})
	d.codex["session"] = &codexRuntime{responded: map[string]string{`1`: `{"decision":"accept"}`}, pending: map[string]bool{}}
	id := json.RawMessage(`1`)
	if err := d.RespondCodexRequest(t.Context(), "session", id, map[string]any{"decision": "accept"}); err != nil {
		t.Fatalf("same decision should be idempotent: %v", err)
	}
	if err := d.RespondCodexRequest(t.Context(), "session", id, map[string]any{"decision": "decline"}); !errors.Is(err, ErrCodexRequestAlreadyAnswered) {
		t.Fatalf("conflicting decision: %v", err)
	}
}
