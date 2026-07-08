package timeline

import (
	"strings"
	"testing"
)

func TestParseClaudeJSONLEmitsTimelineEvents(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-06-30T00:00:00Z","message":{"role":"user","content":[{"type":"text","text":"run ls"},{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"I will inspect files"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":12,"output_tokens":4}}}`,
		`{"type":"approval_request","approval_id":"a1","tool":"Bash","input_json":"{\"command\":\"ls\"}"}`,
		`{"type":"agent_done","event_name":"Stop"}`,
		`{"type":"anomaly","rule_name":"fork-bomb","evidence":"pattern"}`,
		`{"type":"snapshot","snapshot_action":"take","snapshot_name":"snap1"}`,
		"",
	}, "\n")
	events, err := Parse(strings.NewReader(body), Options{SessionID: "s1", ProviderSessionID: "claude-s1", Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	assertKindCount(t, events, KindTurn, 2)
	assertKindCount(t, events, KindToolCall, 1)
	assertKindCount(t, events, KindToolResult, 1)
	assertKindCount(t, events, KindApproval, 1)
	assertKindCount(t, events, KindHookFired, 1)
	assertKindCount(t, events, KindAnomaly, 1)
	assertKindCount(t, events, KindSnapshot, 1)
	assertKindCount(t, events, KindCost, 1)
	tool := firstKind(t, events, KindToolCall)
	if tool.ToolName != "Bash" || tool.ToolID != "toolu_1" || tool.Turn != 2 {
		t.Fatalf("tool event = %#v", tool)
	}
	cost := firstKind(t, events, KindCost)
	if cost.Model != "claude-sonnet-4-5" || cost.InputTokens != 12 || cost.OutputTokens != 4 || cost.TotalInputTokens != 12 || cost.TotalOutputTokens != 4 {
		t.Fatalf("cost event = %#v", cost)
	}
	if events[0].SessionID != "s1" || events[0].ProviderSessionID != "claude-s1" || events[0].Agent != "claude" || events[0].TS != "2026-06-30T00:00:00Z" {
		t.Fatalf("metadata = %#v", events[0])
	}
}

func TestParseRejectsBadJSON(t *testing.T) {
	_, err := Parse(strings.NewReader("{bad}\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "timeline line 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestParsePreservesEventOrderingAndSummaryBounds(t *testing.T) {
	longText := strings.Repeat("x", 220)
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-06-30T00:00:00Z","message":{"role":"user","content":"` + longText + `"}}`,
		`{"type":"assistant","timestamp":"2026-06-30T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`,
		"",
	}, "\n")
	events, err := Parse(strings.NewReader(body), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Offset < events[i-1].Offset {
			t.Fatalf("offset order broke at %d: %#v", i, events)
		}
	}
	if got := len([]rune(events[0].Summary)); got != 160 {
		t.Fatalf("summary length = %d summary=%q", got, events[0].Summary)
	}
	if events[0].Turn != 1 || events[1].Turn != 2 || events[2].Kind != KindToolCall || events[2].Turn != 2 {
		t.Fatalf("turn ordering = %#v", events)
	}
}

func assertKindCount(t *testing.T, events []TimelineEvent, kind EventKind, want int) {
	t.Helper()
	got := 0
	for _, ev := range events {
		if ev.Kind == kind {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s count = %d want %d events=%#v", kind, got, want, events)
	}
}

func firstKind(t *testing.T, events []TimelineEvent, kind EventKind) TimelineEvent {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == kind {
			return ev
		}
	}
	t.Fatalf("missing kind %s in %#v", kind, events)
	return TimelineEvent{}
}
