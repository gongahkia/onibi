package goose

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

func TestInstallWritesOpenPluginsSchemaCleanHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	t.Setenv("ONIBI_GOOSE_HOOKS", path)
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
	hooks := m["hooks"].(map[string]any)
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop"} {
		group := hooks[ev].([]any)[0].(map[string]any)
		for k := range group {
			switch k {
			case "matcher", "hooks":
			default:
				t.Fatalf("%s group has Open Plugins-unknown key %q", ev, k)
			}
		}
		hook := group["hooks"].([]any)[0].(map[string]any)
		for k := range hook {
			switch k {
			case "type", "command":
			default:
				t.Fatalf("%s hook has Open Plugins-unknown key %q", ev, k)
			}
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
