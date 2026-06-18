package gemini

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
