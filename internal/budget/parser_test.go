package budget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeParserReadsAndTailsUsage(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "s-claude"
	transcript := filepath.Join(base, "projects", key, "sessions", sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := strings.Join([]string{
		`{"type":"system","cwd":"/tmp/repo"}`,
		`{"type":"assistant","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":3}}}`,
		`{"type":"assistant","model":"claude-opus-4-7","usage":{"input_tokens":7,"output_tokens":5}}`,
		"",
	}, "\n")
	if err := os.WriteFile(transcript, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := NewClaudeParser(base)
	ref := SessionRef{SessionID: "onibi-s1", ProviderSessionID: sessionID, Agent: "claude", CWD: cwd}
	ev, ok, err := parser.Update(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no cost event")
	}
	if ev.InputTokens != 17 || ev.OutputTokens != 8 || ev.TotalInputTokens != 17 || ev.TotalOutputTokens != 8 || ev.Turns != 2 || ev.Model != "claude-opus-4-7" {
		t.Fatalf("event = %#v", ev)
	}
	if ev.SessionID != "onibi-s1" || ev.ProviderSessionID != sessionID || ev.Agent != "claude" || ev.Transcript != transcript || ev.Offset <= 0 {
		t.Fatalf("event metadata = %#v", ev)
	}
	current, ok, err := parser.Current(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || current.TotalInputTokens != 17 || current.TotalOutputTokens != 8 || current.Model != "claude-opus-4-7" {
		t.Fatalf("current = %#v ok=%v", current, ok)
	}
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"message":{"model":"claude-haiku-4-5","usage":{"input_tokens":2,"output_tokens":11}}}` + "\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ev, ok, err = parser.Update(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no second cost event")
	}
	if ev.InputTokens != 2 || ev.OutputTokens != 11 || ev.TotalInputTokens != 19 || ev.TotalOutputTokens != 19 || ev.Turns != 1 || ev.Model != "claude-haiku-4-5" {
		t.Fatalf("second event = %#v", ev)
	}
	ev, ok, err = parser.Update(ref)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("unexpected event = %#v", ev)
	}
}

func TestClaudeParserFindsFlatProjectTranscript(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(base, "projects", key, "abc.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(`{"usage":{"input_tokens":1,"output_tokens":2},"model":"m"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := NewClaudeParser(base)
	got, err := parser.FindTranscript(SessionRef{ProviderSessionID: "abc", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if got != transcript {
		t.Fatalf("path = %q", got)
	}
}

func TestParseUsageLineSkipsRowsWithoutUsage(t *testing.T) {
	if _, ok, err := ParseUsageLine([]byte(`{"type":"user","message":{"content":"hi"}}`)); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, ok, err := ParseUsageLine([]byte(`{"usage":{"cache_creation_input_tokens":1}}`)); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
