package claude

import (
	"encoding/json"
	"io"

	"github.com/gongahkia/onibi/internal/intake"
)

const MaxPreToolUsePayload = 1 << 20

type PreToolUseResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

type hookSpecific struct {
	HookEventName            string         `json:"hookEventName"`
	PermissionDecision       string         `json:"permissionDecision"`
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
}

// ParsePreToolUse reads Claude Code's PreToolUse stdin payload and returns
// the tool name plus raw tool_input JSON. Invalid or empty input falls back
// to an empty object so the daemon still receives a valid request.
func ParsePreToolUse(r io.Reader) (string, string) {
	b, err := io.ReadAll(io.LimitReader(r, MaxPreToolUsePayload))
	if err != nil || len(b) == 0 {
		return "", "{}"
	}
	var top struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(b, &top); err != nil {
		return "", "{}"
	}
	if len(top.ToolInput) == 0 {
		top.ToolInput = json.RawMessage("{}")
	}
	return top.ToolName, string(top.ToolInput)
}

// PreToolUseResponse maps the daemon approval response onto Claude Code's
// expected hook stdout/stderr/exit-code contract.
func PreToolUseResponse(resp intake.Response) PreToolUseResult {
	switch resp.Decision {
	case "approve":
		return PreToolUseResult{Stdout: marshalHook(hookSpecific{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
		})}
	case "edited":
		var updated map[string]any
		if resp.UpdatedInput != "" {
			_ = json.Unmarshal([]byte(resp.UpdatedInput), &updated)
		}
		return PreToolUseResult{Stdout: marshalHook(hookSpecific{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "allow",
			PermissionDecisionReason: "edited by owner via Telegram",
			UpdatedInput:             updated,
		})}
	case "deny":
		reason := resp.Reason
		if reason == "" {
			reason = "denied by owner via Telegram"
		}
		return PreToolUseResult{
			Stdout: marshalHook(hookSpecific{
				HookEventName:            "PreToolUse",
				PermissionDecision:       "deny",
				PermissionDecisionReason: reason,
			}),
			Stderr:   reason + "\n",
			ExitCode: 2,
		}
	case "expired":
		return PreToolUseResult{
			Stdout: marshalHook(hookSpecific{
				HookEventName:            "PreToolUse",
				PermissionDecision:       "deny",
				PermissionDecisionReason: "approval expired",
			}),
			Stderr:   "approval expired (no decision within 5 min)\n",
			ExitCode: 2,
		}
	default:
		return PreToolUseResult{}
	}
}

func marshalHook(spec hookSpecific) string {
	b, _ := json.Marshal(hookOutput{HookSpecificOutput: spec})
	return string(b) + "\n"
}
