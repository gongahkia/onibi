package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/adapters/denytest"
	"github.com/gongahkia/onibi/internal/store"
)

func newTestEnv(t *testing.T) (*store.DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	t.Setenv("ONIBI_GEMINI_SETTINGS", path)
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return db, notify, path
}

func TestInstallWritesGeminiSchemaCleanHooks(t *testing.T) {
	db, notify, path := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	if fields := legacyFields(m); len(fields) > 0 {
		t.Fatalf("legacy metadata fields written: %v", fields)
	}
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"SessionStart", "BeforeAgent", "BeforeTool", "AfterTool", "Notification", "AfterAgent", "SessionEnd"} {
		group := hooks[ev].([]any)[0].(map[string]any)
		hook := group["hooks"].([]any)[0].(map[string]any)
		for k := range hook {
			switch k {
			case "type", "command", "name", "timeout", "description":
			default:
				t.Fatalf("%s hook has Gemini-unknown key %q", ev, k)
			}
		}
		timeout := int(hook["timeout"].(float64))
		if timeout < 1000 {
			t.Fatalf("%s timeout = %d, want milliseconds", ev, timeout)
		}
		cmd := hook["command"].(string)
		if !strings.Contains(cmd, common.VersionEnv+"=\""+common.IntegrationVersion+"\"") {
			t.Fatalf("%s command missing version env: %s", ev, cmd)
		}
	}
	if got := installedVersion(path); got != common.IntegrationVersion {
		t.Fatalf("installedVersion = %q", got)
	}
}

func TestAdapterGeminiDenyBlocksTool(t *testing.T) {
	notify := denytest.DenyNotify(t)
	target := denytest.Target(t, Agent)
	h := hook(notify, eventSpec{event: "BeforeTool", matcher: "*", typ: "approval_request", wait: true, response: "provider", timeout: 360000})
	cmd := h["command"].(string)
	res := denytest.RunHook(t, cmd, `{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"touch `+target+`"}}`)
	allowed := !strings.Contains(res.Stdout, `"decision":"deny"`)
	if res.Code != 0 || allowed {
		t.Fatalf("deny hook did not return provider block: code=%d stdout=%q stderr=%q", res.Code, res.Stdout, res.Stderr)
	}
	denytest.CreateIfAllowed(t, target, allowed)
}

func TestGeminiUnavailableNotifyReturnsWarningExit(t *testing.T) {
	target := denytest.Target(t, Agent)
	h := hook(filepath.Join(t.TempDir(), "missing-onibi-notify"), eventSpec{event: "BeforeTool", matcher: "*", typ: "approval_request", wait: true, response: "provider", timeout: 360000})
	res := denytest.RunHook(t, h["command"].(string), `{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"touch `+target+`"}}`)
	if res.Code == 0 || res.Code == 2 || res.Stdout != "" {
		t.Fatalf("unavailable notifier result=%+v", res)
	}
	if err := os.WriteFile(target, []byte("created\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiContractHooksCoverLifecycleTimeoutAndDrift(t *testing.T) {
	db, notify, _ := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	expected, err := ExpectedHooks(notify)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(events) || len(observed) != len(events) {
		t.Fatalf("expected=%+v observed=%+v", expected, observed)
	}
	want := map[string]string{
		"SessionStart": "agent_message",
		"BeforeAgent":  "agent_message",
		"BeforeTool":   "approval_request",
		"AfterTool":    "agent_message",
		"Notification": "agent_message",
		"AfterAgent":   "agent_done",
		"SessionEnd":   "session_exited",
	}
	for _, hook := range expected {
		typ, ok := want[hook.Event]
		if !ok || hook.Type != "command" || !strings.Contains(hook.Command, "--type "+typ) {
			t.Fatalf("hook=%+v", hook)
		}
		if hook.Event == "BeforeTool" {
			if hook.Matcher != "*" || hook.Timeout != 360000 || !strings.Contains(hook.Command, "--wait --response provider") {
				t.Fatalf("approval hook=%+v", hook)
			}
		} else if hook.Timeout != 30000 {
			t.Fatalf("lifecycle hook=%+v", hook)
		}
	}
	for _, hook := range observed {
		if !hook.Managed || hook.Type != "command" || hook.Command == "" || hook.Timeout < 1000 {
			t.Fatalf("observed hook=%+v", hook)
		}
	}
	if err := VerifyHash(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiObservedHooksAndStatusReportDisableAndSchemaDrift(t *testing.T) {
	db, notify, path := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	cfg := readHookFile(t, path)
	cfg["hooksConfig"] = map[string]any{"enabled": false}
	hooks := cfg["hooks"].(map[string]any)
	group := hooks["BeforeTool"].([]any)[0].(map[string]any)
	hook := group["hooks"].([]any)[0].(map[string]any)
	hook["timeout"] = "slow"
	hook["unknown"] = true
	writeHookFile(t, path, cfg)
	observed, err := ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"schema-invalid: timeout is not a positive number", "schema-invalid: unknown hook field unknown"} {
		if !hasObservedProblem(observed, want) {
			t.Fatalf("missing %q in %+v", want, observed)
		}
	}
	info := Status(context.Background(), db)
	if !info.Disabled || !strings.Contains(info.Message, "hooksConfig.enabled=false") || info.Next == "" {
		t.Fatalf("status=%+v", info)
	}
}

func TestGeminiUninstallMissingSettingsIsNoop(t *testing.T) {
	db, _, path := newTestEnv(t)
	if err := Uninstall(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstall created settings: %v", err)
	}
}

func TestGeminiInstallAndUninstallKeepOneOriginalBackup(t *testing.T) {
	db, notify, path := newTestEnv(t)
	writeHookFile(t, path, map[string]any{"custom": true, "hooks": map[string]any{}})
	ctx := t.Context()
	if err := Install(ctx, db, notify); err != nil {
		t.Fatal(err)
	}
	if err := Install(ctx, db, notify); err != nil {
		t.Fatal(err)
	}
	var backups int
	if err := db.SQL().QueryRowContext(ctx, "SELECT count(*) FROM hook_backups WHERE agent = ? AND path = ?", Agent, path).Scan(&backups); err != nil {
		t.Fatal(err)
	}
	if backups != 1 {
		t.Fatalf("backups after idempotent install=%d", backups)
	}
	info := Status(ctx, db)
	if !info.Installed || !info.Managed || info.Tampered || info.MinimumProviderVersion != MinimumProviderVersion {
		t.Fatalf("status=%+v", info)
	}
	if err := Uninstall(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT count(*) FROM hook_backups WHERE agent = ? AND path = ?", Agent, path).Scan(&backups); err != nil {
		t.Fatal(err)
	}
	if backups != 1 {
		t.Fatalf("backups after uninstall=%d", backups)
	}
	if cfg := readHookFile(t, path); cfg["custom"] != true {
		t.Fatalf("settings=%#v", cfg)
	}
}

func TestInstallMigratesLegacyGeminiMetadata(t *testing.T) {
	db, notify, path := newTestEnv(t)
	legacy := map[string]any{
		common.VersionField: "1.0.0",
		"hooks": map[string]any{
			"AfterTool": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							common.GuardField:   true,
							common.VersionField: "1.0.0",
							"type":              "command",
							"command":           `exec "/tmp/onibi-notify" --agent gemini --format gemini --type agent_message`,
							"timeout":           30,
						},
						map[string]any{"type": "command", "command": "echo user-hook"},
					},
				},
			},
		},
	}
	writeHookFile(t, path, legacy)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	if fields := legacyFields(m); len(fields) > 0 {
		t.Fatalf("legacy metadata fields remain: %v", fields)
	}
	foundUser := false
	for _, group := range m["hooks"].(map[string]any)["AfterTool"].([]any) {
		for _, h := range group.(map[string]any)["hooks"].([]any) {
			cmd, _ := h.(map[string]any)["command"].(string)
			if cmd == "echo user-hook" {
				foundUser = true
			}
		}
	}
	if !foundUser {
		t.Fatal("user hook was not preserved")
	}
}

func readHookFile(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeHookFile(t *testing.T, path string, m map[string]any) {
	t.Helper()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func legacyFields(v any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == common.VersionField || k == common.GuardField {
					out = append(out, k)
				}
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(v)
	return out
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
