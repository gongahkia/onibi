package copilot

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

func TestInstallWritesCopilotSchemaCleanHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onibi.json")
	t.Setenv("ONIBI_COPILOT_HOOK", path)
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	if fields := legacyFields(m); len(fields) > 0 {
		t.Fatalf("legacy metadata fields written: %v", fields)
	}
	if v, _ := m["version"].(float64); v != 1 {
		t.Fatalf("version = %v", m["version"])
	}
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"sessionStart", "userPromptSubmitted", "preToolUse", "postToolUse", "postToolUseFailure", "notification", "agentStop", "sessionEnd", "errorOccurred"} {
		hook := hooks[ev].([]any)[0].(map[string]any)
		for k := range hook {
			switch k {
			case "type", "bash", "powershell", "command", "cwd", "env", "timeout", "timeoutSec":
			default:
				t.Fatalf("%s hook has Copilot-unknown key %q", ev, k)
			}
		}
		cmd := hook["bash"].(string)
		if !strings.Contains(cmd, common.VersionEnv+"=\""+common.IntegrationVersion+"\"") {
			t.Fatalf("%s command missing version env: %s", ev, cmd)
		}
	}
	if got := installedVersion(path); got != common.IntegrationVersion {
		t.Fatalf("installedVersion = %q", got)
	}
}

func TestExpectedAndObservedHooksReportCopilotDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onibi.json")
	t.Setenv("ONIBI_COPILOT_HOOK", path)
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	expected, err := ExpectedHooks(notify)
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(events) {
		t.Fatalf("expected hooks = %d", len(expected))
	}
	observed, err := ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(events) {
		t.Fatalf("observed hooks = %d", len(observed))
	}
	for _, row := range observed {
		if !row.Managed || row.Type != "command" || row.Command == "" {
			t.Fatalf("bad observed hook: %+v", row)
		}
	}

	m := readHookFile(t, path)
	m[common.VersionField] = "legacy"
	hooks := m["hooks"].(map[string]any)
	hook := hooks["preToolUse"].([]any)[0].(map[string]any)
	hook["onibiManaged"] = true
	writeHookFile(t, path, m)
	observed, err = ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(observed, "schema-invalid: unknown top-level field "+common.VersionField) {
		t.Fatalf("missing top-level schema problem: %+v", observed)
	}
	if !hasProblem(observed, "schema-invalid: unknown hook field onibiManaged") {
		t.Fatalf("missing hook schema problem: %+v", observed)
	}
}

func TestStatusDetectsDisableAllHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onibi.json")
	t.Setenv("ONIBI_COPILOT_HOOK", path)
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	m := readHookFile(t, path)
	m["disableAllHooks"] = true
	writeHookFile(t, path, m)
	info := Status(context.Background(), db)
	if !info.Disabled || !strings.Contains(info.Message, "disableAllHooks=true") {
		t.Fatalf("status = %+v", info)
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
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hasProblem(rows []common.ObservedHook, want string) bool {
	for _, row := range rows {
		for _, problem := range row.Problems {
			if problem == want {
				return true
			}
		}
	}
	return false
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
