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
