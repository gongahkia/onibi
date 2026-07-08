package codex

import (
	"context"
	"encoding/json"
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
	path := filepath.Join(dir, "hooks.json")
	t.Setenv("ONIBI_CODEX_HOOKS", path)
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

func TestInstallWritesCodexSchemaCleanHooks(t *testing.T) {
	db, notify, path := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	if _, ok := m[common.VersionField]; ok {
		t.Fatalf("top-level %s must not be written", common.VersionField)
	}
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"SessionStart", "PreToolUse", "PostToolUse", "Stop"} {
		groups := hooks[ev].([]any)
		if len(groups) != 1 {
			t.Fatalf("%s groups = %d", ev, len(groups))
		}
		entries := groups[0].(map[string]any)["hooks"].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s entries = %d", ev, len(entries))
		}
		hook := entries[0].(map[string]any)
		for k := range hook {
			switch k {
			case "type", "command", "timeout", "statusMessage":
			default:
				t.Fatalf("%s hook has Codex-unknown key %q", ev, k)
			}
		}
		cmd := hook["command"].(string)
		if !strings.Contains(cmd, versionEnv+"=\""+common.IntegrationVersion+"\"") {
			t.Fatalf("%s command missing version env: %s", ev, cmd)
		}
	}
	info := Status(context.Background(), db)
	if info.Outdated || info.InstalledVersion == nil || *info.InstalledVersion != common.IntegrationVersion {
		t.Fatalf("bad status: %+v", info)
	}
}

func TestAdapterCodexDenyBlocksTool(t *testing.T) {
	notify := denytest.DenyNotify(t)
	target := denytest.Target(t, Agent)
	h := hook(notify, eventSpec{event: "PreToolUse", typ: "approval_request", wait: true, response: "provider"})
	cmd := h["command"].(string)
	res := denytest.RunHook(t, cmd, `{"tool_name":"shell","tool_input":{"command":"touch `+target+`"}}`)
	if res.Code == 0 || !strings.Contains(res.Stdout, `"permissionDecision":"deny"`) {
		t.Fatalf("deny hook did not block: code=%d stdout=%q stderr=%q", res.Code, res.Stdout, res.Stderr)
	}
	denytest.CreateIfAllowed(t, target, res.Code == 0)
}

func TestInstallRemovesLegacyCodexMetadataAndPreservesUserHooks(t *testing.T) {
	db, notify, path := newTestEnv(t)
	legacy := map[string]any{
		common.VersionField: "1.0.0",
		"hooks": map[string]any{
			"PostToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							common.GuardField:   true,
							common.VersionField: "1.0.0",
							"type":              "command",
							"command":           `exec "/tmp/onibi-notify" --agent codex --format codex --type agent_message`,
						},
						map[string]any{
							"type":    "command",
							"command": "echo user-hook",
						},
					},
				},
			},
		},
	}
	writeHookFile(t, path, legacy)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if strings.Contains(body, common.VersionField) || strings.Contains(body, common.GuardField) {
		t.Fatalf("legacy metadata leaked into Codex hooks: %s", body)
	}
	m := readHookFile(t, path)
	hooks := m["hooks"].(map[string]any)
	foundUser := false
	for _, group := range hooks["PostToolUse"].([]any) {
		for _, entry := range group.(map[string]any)["hooks"].([]any) {
			cmd, _ := entry.(map[string]any)["command"].(string)
			if cmd == "echo user-hook" {
				foundUser = true
			}
		}
	}
	if !foundUser {
		t.Fatal("user hook was not preserved")
	}
	if got := InstalledVersion(path); got != common.IntegrationVersion {
		t.Fatalf("InstalledVersion = %q", got)
	}
}

func TestInstallCodexReinstallDoesNotDuplicateManagedHooks(t *testing.T) {
	db, notify, path := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"SessionStart", "PreToolUse", "PostToolUse", "Stop"} {
		groups := hooks[ev].([]any)
		if len(groups) != 1 {
			t.Fatalf("%s groups = %d", ev, len(groups))
		}
		entries := groups[0].(map[string]any)["hooks"].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s entries = %d", ev, len(entries))
		}
	}
}

func TestUninstallRemovesCommandManagedCodexHooks(t *testing.T) {
	db, notify, path := newTestEnv(t)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	hooks := m["hooks"].(map[string]any)
	if len(hooks) != 0 {
		t.Fatalf("managed hooks remain: %+v", hooks)
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
