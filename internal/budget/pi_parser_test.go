package budget

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPiParserReadsAndTailsUsage(t *testing.T) {
	base := t.TempDir()
	providerSessionID := "a6d1f8f3-75d4-4b2f-93d2-1ed8ddda89f8"
	transcript := filepath.Join(base, "--tmp-repo--", "2026-07-15T00-00-00-000Z_"+providerSessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := strings.Join([]string{
		`{"type":"session","version":3,"id":"` + providerSessionID + `","cwd":"/tmp/repo"}`,
		`{"type":"message","message":{"role":"assistant","model":"anthropic/claude-sonnet-4-5","usage":{"input":12,"output":4,"cacheRead":8,"cacheWrite":1,"totalTokens":25}}}`,
		`{"type":"message","message":{"role":"user","content":"ignore"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(transcript, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := NewPiParser(base)
	ref := SessionRef{SessionID: "onibi-s1", ProviderSessionID: providerSessionID, Agent: "pi"}
	ev, ok, err := parser.Update(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no cost event")
	}
	if ev.Agent != "pi" || ev.InputTokens != 12 || ev.OutputTokens != 4 || ev.TotalInputTokens != 12 || ev.TotalOutputTokens != 4 || ev.Model != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("event = %#v", ev)
	}
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"message","message":{"role":"assistant","model":"openai/gpt-5","usage":{"input":3,"output":7,"totalTokens":10}}}` + "\n"); err != nil {
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
	if !ok || ev.InputTokens != 3 || ev.OutputTokens != 7 || ev.TotalInputTokens != 15 || ev.TotalOutputTokens != 11 || ev.Model != "openai/gpt-5" {
		t.Fatalf("second event = %#v ok=%v", ev, ok)
	}
}

func TestPiParserRejectsHeaderMismatchAndBadUsage(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "2026_expected.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","id":"other"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPiParser(base).FindTranscript(SessionRef{ProviderSessionID: "expected"}); !os.IsNotExist(err) {
		t.Fatalf("err = %v", err)
	}
	if _, ok, err := ParsePiUsageLine([]byte(`{"type":"message","message":{"role":"assistant","usage":{"input":-1,"output":2}}}`)); err == nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestPiParserUsesProjectSessionDir(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	project := t.TempDir()
	sessionDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pi", "settings.json"), []byte(`{"sessionDir":`+strconv.Quote(sessionDir)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	providerSessionID := "a6d1f8f3-75d4-4b2f-93d2-1ed8ddda89f8"
	path := filepath.Join(sessionDir, "2026_"+providerSessionID+".jsonl")
	body := `{"type":"session","id":"` + providerSessionID + `"}` + "\n" + `{"type":"message","message":{"role":"assistant","model":"m","usage":{"input":1,"output":2}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ev, ok, err := NewPiParser("").Update(SessionRef{SessionID: "onibi", ProviderSessionID: providerSessionID, CWD: project})
	if err != nil || !ok || ev.TotalInputTokens != 1 || ev.TotalOutputTokens != 2 {
		t.Fatalf("event=%#v ok=%v err=%v", ev, ok, err)
	}
}
