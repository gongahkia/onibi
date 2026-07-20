package goose

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
	for _, ev := range eventNames() {
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
			case "type", "command", "timeout":
			default:
				t.Fatalf("%s hook has Open Plugins-unknown key %q", ev, k)
			}
		}
		cmd := hook["command"].(string)
		if !strings.Contains(cmd, common.VersionEnv+"=\""+common.IntegrationVersion+"\"") {
			t.Fatalf("%s command missing version env: %s", ev, cmd)
		}
		if got, _ := hook["timeout"].(float64); int(got) != eventSpecForName(ev).timeout {
			t.Fatalf("%s timeout = %v", ev, hook["timeout"])
		}
	}
	if got := installedVersion(path); got != common.IntegrationVersion {
		t.Fatalf("installedVersion = %q", got)
	}
}

func TestAdapterGooseDenyBlocksTool(t *testing.T) {
	target := denytest.Target(t, Agent)
	h := hook(denytest.DenyNotify(t), eventSpec{event: "PreToolUse", typ: "approval_request", wait: true, response: "provider", timeout: 360})
	cmd := h["command"].(string)
	if !strings.Contains(cmd, "approval_request") || !strings.Contains(cmd, "--wait") || !strings.Contains(cmd, "--response provider") {
		t.Fatalf("goose missing native approval command: %s", cmd)
	}
	res := denytest.RunHook(t, cmd, `{"event":"PreToolUse","session_id":"deny-test","tool_name":"developer__shell","tool_input":{"command":"touch denied"}}`)
	if res.Code != 0 || !strings.Contains(res.Stdout, `"decision":"block"`) {
		t.Fatalf("goose deny result = %+v", res)
	}
	if !strings.Contains(res.Stdout, `"reason":"deny fixture"`) {
		t.Fatalf("goose deny reason = %+v", res)
	}
	denytest.AssertNotCreated(t, target)
}

func TestObservedHooksReportsOpenPluginsSchemaInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	t.Setenv("ONIBI_GOOSE_HOOKS", path)
	cfg := map[string]any{
		common.VersionField: "1.0.0",
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					common.VersionField: "1.0.0",
					"hooks": []any{
						map[string]any{
							"type":              "command",
							"command":           "echo user",
							common.GuardField:   true,
							common.VersionField: "1.0.0",
						},
					},
				},
			},
		},
	}
	writeHookFile(t, path, cfg)
	observed, err := ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"schema-invalid: unknown top-level field " + common.VersionField,
		"schema-invalid: unknown matcher field " + common.VersionField,
		"schema-invalid: unknown hook field " + common.GuardField,
		"schema-invalid: unknown hook field " + common.VersionField,
	} {
		if !hasObservedProblem(observed, want) {
			t.Fatalf("missing %q: %+v", want, observed)
		}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func eventNames() []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.event)
	}
	return out
}

func eventSpecForName(name string) eventSpec {
	for _, e := range events {
		if e.event == name {
			return e
		}
	}
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
