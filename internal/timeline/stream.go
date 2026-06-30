package timeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
)

type EventKind string

const (
	KindTurn       EventKind = "turn"
	KindToolCall   EventKind = "tool_call"
	KindToolResult EventKind = "tool_result"
	KindApproval   EventKind = "approval"
	KindHookFired  EventKind = "hook_fired"
	KindAnomaly    EventKind = "anomaly"
	KindSnapshot   EventKind = "snapshot"
	KindCost       EventKind = "cost"
)

type Options struct {
	SessionID         string
	ProviderSessionID string
	Agent             string
}

type TimelineEvent struct {
	Kind              EventKind      `json:"kind"`
	SessionID         string         `json:"session_id,omitempty"`
	ProviderSessionID string         `json:"provider_session_id,omitempty"`
	Agent             string         `json:"agent,omitempty"`
	Turn              int            `json:"turn,omitempty"`
	Role              string         `json:"role,omitempty"`
	ToolName          string         `json:"tool_name,omitempty"`
	ToolID            string         `json:"tool_id,omitempty"`
	Model             string         `json:"model,omitempty"`
	Summary           string         `json:"summary,omitempty"`
	TS                string         `json:"ts,omitempty"`
	Offset            int64          `json:"offset"`
	InputTokens       int64          `json:"input_tokens,omitempty"`
	OutputTokens      int64          `json:"output_tokens,omitempty"`
	TotalInputTokens  int64          `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int64          `json:"total_output_tokens,omitempty"`
	Payload           map[string]any `json:"payload,omitempty"`
}

func ParseFile(path string, opts Options) ([]TimelineEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, opts)
}

func Parse(r io.Reader, opts Options) ([]TimelineEvent, error) {
	p := parser{opts: opts}
	br := bufio.NewReaderSize(r, 64*1024)
	var offset int64
	var lineNo int
	var out []TimelineEvent
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 {
			lineNo++
			lineOffset := offset
			offset += int64(len(raw))
			line := strings.TrimSpace(string(raw))
			if line != "" {
				events, parseErr := p.parseLine([]byte(line), lineOffset)
				if parseErr != nil {
					return nil, fmt.Errorf("timeline line %d: %w", lineNo, parseErr)
				}
				out = append(out, events...)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

type parser struct {
	opts        Options
	turn        int
	totalInput  int64
	totalOutput int64
}

func (p *parser) parseLine(line []byte, offset int64) ([]TimelineEvent, error) {
	var top map[string]any
	if err := json.Unmarshal(line, &top); err != nil {
		return nil, err
	}
	base := p.baseEvent(top, offset)
	var out []TimelineEvent
	role := messageRole(top)
	if role != "" {
		p.turn++
		ev := base
		ev.Kind = KindTurn
		ev.Turn = p.turn
		ev.Role = role
		ev.Summary = summarizeContent(messageContent(top))
		ev.Payload = top
		out = append(out, ev)
	}
	for _, ev := range p.toolEvents(top, base) {
		out = append(out, ev)
	}
	if ev, ok, err := p.costEvent(line, base); err != nil {
		return nil, err
	} else if ok {
		out = append(out, ev)
	}
	if ev, ok := p.genericEvent(top, base); ok {
		out = append(out, ev)
	}
	return out, nil
}

func (p *parser) baseEvent(top map[string]any, offset int64) TimelineEvent {
	return TimelineEvent{
		SessionID:         p.opts.SessionID,
		ProviderSessionID: p.opts.ProviderSessionID,
		Agent:             firstNonEmpty(p.opts.Agent, stringAt(top, "agent")),
		Turn:              p.turn,
		TS:                timestampAt(top),
		Offset:            offset,
	}
}

func (p *parser) toolEvents(top map[string]any, base TimelineEvent) []TimelineEvent {
	var out []TimelineEvent
	blocks := contentBlocks(messageContent(top))
	if len(blocks) == 0 {
		switch normalizeType(stringAt(top, "type")) {
		case "tool_use", "tool_call", "tool_result":
			blocks = []map[string]any{top}
		}
	}
	for _, block := range blocks {
		typ := normalizeType(stringAt(block, "type"))
		switch typ {
		case "tool_use", "tool_call":
			ev := base
			ev.Kind = KindToolCall
			ev.Turn = p.turn
			ev.ToolName = firstNonEmpty(stringAt(block, "name"), stringAt(block, "tool"), stringAt(block, "tool_name"))
			ev.ToolID = firstNonEmpty(stringAt(block, "id"), stringAt(block, "tool_use_id"), stringAt(block, "tool_call_id"))
			ev.Summary = firstNonEmpty(ev.ToolName, "tool call")
			ev.Payload = block
			out = append(out, ev)
		case "tool_result":
			ev := base
			ev.Kind = KindToolResult
			ev.Turn = p.turn
			ev.ToolID = firstNonEmpty(stringAt(block, "tool_use_id"), stringAt(block, "tool_call_id"), stringAt(block, "id"))
			ev.Summary = summarizeContent(block["content"])
			if ev.Summary == "" {
				ev.Summary = "tool result"
			}
			ev.Payload = block
			out = append(out, ev)
		}
	}
	return out
}

func (p *parser) costEvent(line []byte, base TimelineEvent) (TimelineEvent, bool, error) {
	usage, ok, err := budget.ParseUsageLine(line)
	if err != nil || !ok {
		return TimelineEvent{}, ok, err
	}
	p.totalInput += usage.InputTokens
	p.totalOutput += usage.OutputTokens
	ev := base
	ev.Kind = KindCost
	ev.Turn = p.turn
	ev.Model = usage.Model
	ev.InputTokens = usage.InputTokens
	ev.OutputTokens = usage.OutputTokens
	ev.TotalInputTokens = p.totalInput
	ev.TotalOutputTokens = p.totalOutput
	ev.Summary = fmt.Sprintf("%d in / %d out", usage.InputTokens, usage.OutputTokens)
	return ev, true, nil
}

func (p *parser) genericEvent(top map[string]any, base TimelineEvent) (TimelineEvent, bool) {
	typ := normalizeType(stringAt(top, "type"))
	ev := base
	ev.Payload = top
	if eventName := stringAt(top, "event_name"); eventName != "" {
		ev.Kind = KindHookFired
		ev.Summary = eventName
		return ev, true
	}
	switch typ {
	case "approval", "approval_request", "approval_requested":
		ev.Kind = KindApproval
		ev.ToolName = stringAt(top, "tool")
		ev.ToolID = stringAt(top, "approval_id")
		ev.Summary = firstNonEmpty(ev.ToolName, ev.ToolID, "approval")
	case "hook", "hook_fired":
		ev.Kind = KindHookFired
		ev.Summary = firstNonEmpty(stringAt(top, "event_name"), stringAt(top, "hook"), "hook")
	case "anomaly", "anomaly_request", "anomaly_requested":
		ev.Kind = KindAnomaly
		ev.Summary = firstNonEmpty(stringAt(top, "rule_name"), stringAt(top, "evidence"), "anomaly")
	case "snapshot":
		ev.Kind = KindSnapshot
		ev.Summary = firstNonEmpty(stringAt(top, "snapshot_name"), stringAt(top, "snapshot_action"), "snapshot")
	default:
		return TimelineEvent{}, false
	}
	return ev, true
}

func messageRole(top map[string]any) string {
	role := firstNonEmpty(nestedString(top, "message", "role"), stringAt(top, "role"))
	if role != "" {
		return role
	}
	switch normalizeType(stringAt(top, "type")) {
	case "user", "assistant", "system":
		return normalizeType(stringAt(top, "type"))
	default:
		return ""
	}
}

func messageContent(top map[string]any) any {
	if msg := objectAt(top, "message"); msg != nil {
		if content, ok := msg["content"]; ok {
			return content
		}
	}
	return top["content"]
}

func contentBlocks(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	default:
		return nil
	}
}

func summarizeContent(v any) string {
	switch x := v.(type) {
	case string:
		return compactSummary(x)
	case []any:
		var parts []string
		for _, item := range x {
			part := summarizeContent(item)
			if part != "" {
				parts = append(parts, part)
			}
		}
		return compactSummary(strings.Join(parts, " "))
	case map[string]any:
		switch normalizeType(stringAt(x, "type")) {
		case "text":
			return compactSummary(stringAt(x, "text"))
		case "tool_use", "tool_call":
			return compactSummary(firstNonEmpty(stringAt(x, "name"), stringAt(x, "tool"), "tool call"))
		case "tool_result":
			return compactSummary(summarizeContent(x["content"]))
		default:
			return compactSummary(firstNonEmpty(stringAt(x, "text"), stringAt(x, "content")))
		}
	default:
		return ""
	}
}

func compactSummary(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= 160 {
		return s
	}
	return string(runes[:157]) + "..."
}

func timestampAt(top map[string]any) string {
	for _, key := range []string{"timestamp", "created_at", "ts"} {
		if s := stringAt(top, key); s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t.UTC().Format(time.RFC3339Nano)
			}
			return s
		}
		if n, ok := numberAt(top, key); ok && n > 0 {
			return time.Unix(n, 0).UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func objectAt(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func nestedString(m map[string]any, first, second string) string {
	if child := objectAt(m, first); child != nil {
		return stringAt(child, second)
	}
	return ""
}

func stringAt(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func numberAt(m map[string]any, key string) (int64, bool) {
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeType(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), ".", "_")
}
