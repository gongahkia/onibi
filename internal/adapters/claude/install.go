package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/catalog"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

const Agent = "claude"
const guardKey = "onibi-managed"

func init() {
	catalog.MustRegister(catalog.BuiltinAgentManifest(Agent, catalog.Adapter{
		Name:              Agent,
		Install:           Install,
		Uninstall:         Uninstall,
		Status:            Status,
		Verify:            VerifyHash,
		Adopt:             Adopt,
		ExpectedHooks:     ExpectedHooks,
		ObservedHooks:     ObservedHooks,
		TrustInstructions: TrustInstructions,
		BackupPath:        BackupPath,
		DetectPresence:    DetectPresence,
	}, map[string]string{"PreToolUse": "*"}))
}

type eventSpec struct {
	event    string
	typ      string
	wait     bool
	response string
	timeout  int
}

var events = []eventSpec{
	{event: "SessionStart", typ: "agent_message", timeout: 30},
	{event: "UserPromptSubmit", typ: "agent_message", timeout: 30},
	{event: "PreToolUse", typ: "approval_request", wait: true, response: "provider", timeout: 360},
	{event: "PostToolUse", typ: "agent_message", timeout: 30},
	{event: "PostToolUseFailure", typ: "agent_message", timeout: 30},
	{event: "Stop", typ: "agent_done", timeout: 5},
	{event: "SessionEnd", typ: "session_exited", timeout: 30},
}

// SettingsPath returns the canonical Claude Code settings.json path:
// $CLAUDE_CONFIG_DIR/settings.json or ~/.claude/settings.json.
func SettingsPath() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func DetectPresence() bool {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return common.PathExists(d)
	}
	return common.HomeExists(".claude")
}

// Install writes the Onibi Stop and PreToolUse hooks into Claude's
// settings.json. Idempotent — previous Onibi-managed entries are replaced
// in place. Other (user-managed) hooks are preserved verbatim.
//
// notifyBin must be an absolute path to onibi-notify.
func Install(ctx context.Context, db *store.DB, notifyBin string) error {
	if !filepath.IsAbs(notifyBin) {
		return errors.New("notifyBin must be absolute")
	}
	if _, err := os.Stat(notifyBin); err != nil {
		return fmt.Errorf("notify binary missing: %w", err)
	}

	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	existing, err := readJSON(path)
	if err != nil {
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}

	merged := existing
	for _, e := range events {
		merged = mergeEventHook(merged, e.event, buildEventHook(notifyBin, e))
	}
	if err := writeJSON(path, merged); err != nil {
		return err
	}

	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	return common.Record(ctx, db, Agent, path, body)
}

// Uninstall removes all Onibi-managed hooks from Claude's settings.json.
func Uninstall(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	existing, err := readJSON(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	cleaned := existing
	for _, e := range events {
		cleaned = removeEventHook(cleaned, e.event)
	}
	if err := writeJSON(path, cleaned); err != nil {
		return err
	}
	return common.DeleteRecord(ctx, db, Agent, path)
}

func Status(ctx context.Context, db *store.DB) common.Info {
	path, err := SettingsPath()
	if err != nil {
		return common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	info := common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, InstallPath: path}
	body, err := ManagedBody(path)
	if err != nil {
		if strings.Contains(err.Error(), "onibi-managed Claude hook is missing") {
			common.MarkNotInstalled(&info)
			return info
		}
		info.Message = err.Error()
		return info
	}
	version := InstalledVersion(path)
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Claude hooks installed", "onibi install-hooks --agent claude")
	return info
}

// VerifyHash returns nil iff the currently installed hook block (Stop +
// PreToolUse) matches what was recorded at install time.
func VerifyHash(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
}

func Adopt(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	return common.Record(ctx, db, Agent, path, body)
}

func ManagedBody(path string) ([]byte, error) {
	existing, err := readJSON(path)
	if err != nil {
		return nil, err
	}
	managed := make([]any, 0, len(events))
	var missing []string
	for _, e := range events {
		hook := extractEventHook(existing, e.event)
		if hook == nil {
			missing = append(missing, e.event)
			continue
		}
		managed = append(managed, map[string]any{"event": e.event, "hook": hook})
	}
	if len(missing) > 0 {
		return nil, errors.New("onibi-managed Claude hook is missing: " + strings.Join(missing, ", "))
	}
	return common.StableJSON(managed), nil
}

func InstalledVersion(path string) string {
	existing, err := readJSON(path)
	if err != nil {
		return ""
	}
	for _, e := range events {
		hook := extractEventHook(existing, e.event)
		if hook == nil {
			continue
		}
		if s, _ := hook[common.VersionField].(string); s != "" {
			return s
		}
		for _, h := range asSlice(hook["hooks"]) {
			hm, _ := h.(map[string]any)
			if s := common.CommandVersion(commandValue(hm)); s != "" {
				return s
			}
		}
	}
	return ""
}

func ExpectedHooks(notifyBin string) ([]common.ExpectedHook, error) {
	out := make([]common.ExpectedHook, 0, len(events))
	for _, e := range events {
		h := buildEventHook(notifyBin, e)
		hooks := h["hooks"].([]any)
		hm := hooks[0].(map[string]any)
		cmd, _ := hm["command"].(string)
		out = append(out, common.ExpectedHook{
			Event:   e.event,
			Matcher: "",
			Type:    "command",
			Command: cmd,
			Timeout: e.timeout,
		})
	}
	return out, nil
}

func ObservedHooks() ([]common.ObservedHook, error) {
	path, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	cfg, err := readJSON(path)
	if err != nil {
		return nil, err
	}
	var out []common.ObservedHook
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		if _, exists := cfg["hooks"]; exists {
			out = append(out, common.ObservedHook{Event: "*", Problems: []string{"schema-invalid: hooks is not an object"}})
		}
		return out, nil
	}
	for _, event := range common.SortStrings(keys(hooks)) {
		for _, group := range asSlice(hooks[event]) {
			gm, ok := group.(map[string]any)
			if !ok {
				out = append(out, common.ObservedHook{Event: event, Problems: []string{"schema-invalid: matcher group is not an object"}})
				continue
			}
			groupProblems := groupSchemaProblems(gm)
			matcher, _ := gm["matcher"].(string)
			inner := asSlice(gm["hooks"])
			if inner == nil {
				out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(groupProblems, "schema-invalid: hooks is not an array")})
				continue
			}
			for _, h := range inner {
				hm, ok := h.(map[string]any)
				if !ok {
					out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(groupProblems, "schema-invalid: hook is not an object")})
					continue
				}
				typ, _ := hm["type"].(string)
				cmd, _ := hm["command"].(string)
				status, _ := hm["statusMessage"].(string)
				out = append(out, common.ObservedHook{
					Event:         event,
					Matcher:       matcher,
					Type:          typ,
					Command:       cmd,
					Timeout:       intValue(hm["timeout"]),
					StatusMessage: status,
					Managed:       isManagedHook(hm),
					Problems:      append(groupProblems, hookSchemaProblems(hm)...),
				})
			}
		}
	}
	return out, nil
}

func TrustInstructions() []string {
	return []string{
		"Claude next step: run claude, open /hooks, inspect onibi-notify commands, then keep them enabled if they match.",
		"Use onibi hooks --show --agent claude to compare expected commands, installed commands, backups, and drift.",
	}
}

func BackupPath(ctx context.Context, db *store.DB) string {
	path, err := SettingsPath()
	if err != nil {
		return ""
	}
	backup, ok, err := common.LatestBackup(ctx, db, Agent, path)
	if err != nil || !ok {
		return ""
	}
	return backup.BackupPath
}

// ----------------------------------------------------------------------------
// JSON I/O + hook merge logic
// ----------------------------------------------------------------------------

func readJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeJSON(path string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func buildEventHook(notifyBin string, e eventSpec) map[string]any {
	return map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": common.VersionedCommand(notifyBin, Agent, Agent, e.typ, e.wait, e.response),
				"timeout": e.timeout,
			},
		},
	}
}

// mergeEventHook adds our entry to settings.hooks.<eventName>, replacing
// any prior Onibi-managed entry and leaving user-managed entries untouched.
func mergeEventHook(settings map[string]any, eventName string, ours map[string]any) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	existing, _ := hooks[eventName].([]any)
	kept := existing[:0]
	for _, e := range existing {
		m, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		if isManagedEventHook(m) {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, ours)
	hooks[eventName] = kept
	settings["hooks"] = hooks
	return settings
}

func removeEventHook(settings map[string]any, eventName string) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return settings
	}
	existing, _ := hooks[eventName].([]any)
	kept := existing[:0]
	for _, e := range existing {
		if m, ok := e.(map[string]any); ok && isManagedEventHook(m) {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(hooks, eventName)
	} else {
		hooks[eventName] = kept
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	return settings
}

func extractEventHook(settings map[string]any, eventName string) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	existing, _ := hooks[eventName].([]any)
	for _, e := range existing {
		if m, ok := e.(map[string]any); ok && isManagedEventHook(m) {
			return m
		}
	}
	return nil
}

func isManagedEventHook(m map[string]any) bool {
	if m[guardKey] == true {
		return true
	}
	for _, h := range asSlice(m["hooks"]) {
		hm, _ := h.(map[string]any)
		if isManagedHook(hm) {
			return true
		}
	}
	return false
}

func isManagedHook(m map[string]any) bool {
	return strings.Contains(commandValue(m), "onibi-notify") && strings.Contains(commandValue(m), "--agent "+Agent)
}

func commandValue(m map[string]any) string {
	cmd, _ := m["command"].(string)
	return cmd
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func groupSchemaProblems(m map[string]any) []string {
	var problems []string
	for k := range m {
		switch k {
		case "matcher", "hooks":
		default:
			problems = append(problems, "schema-invalid: unknown matcher field "+k)
		}
	}
	return problems
}

func hookSchemaProblems(m map[string]any) []string {
	var problems []string
	for k := range m {
		switch k {
		case "type", "if", "timeout", "statusMessage", "once", "command", "args", "async", "asyncRewake", "shell", "url", "headers", "allowedEnvVars", "server", "tool", "input", "prompt", "model":
		default:
			problems = append(problems, "schema-invalid: unknown hook field "+k)
		}
	}
	return problems
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
