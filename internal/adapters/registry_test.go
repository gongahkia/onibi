package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func TestRegistryAdaptersInstallAndVerify(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("ONIBI_CODEX_HOOKS", filepath.Join(dir, "codex-hooks.json"))
	t.Setenv("ONIBI_GEMINI_SETTINGS", filepath.Join(dir, "gemini-settings.json"))
	t.Setenv("ONIBI_COPILOT_HOOK", filepath.Join(dir, "copilot-hooks.json"))
	t.Setenv("ONIBI_GOOSE_HOOKS", filepath.Join(dir, "goose-hooks.json"))
	t.Setenv("ONIBI_OPENCODE_PLUGIN", filepath.Join(dir, "opencode.js"))
	t.Setenv("ONIBI_PI_EXTENSION", filepath.Join(dir, "pi.ts"))
	t.Setenv("ONIBI_AMP_PLUGIN", filepath.Join(dir, "amp.ts"))

	for _, name := range Names() {
		a, ok := Get(name)
		if !ok {
			t.Fatalf("missing adapter %s", name)
		}
		if err := a.Install(context.Background(), db, notify); err != nil {
			t.Fatalf("%s install: %v", name, err)
		}
		if err := a.Verify(context.Background(), db); err != nil {
			t.Fatalf("%s verify: %v", name, err)
		}
		info := a.Status(context.Background(), db)
		if !info.Installed {
			t.Fatalf("%s status not installed: %+v", name, info)
		}
		if info.InstallPath == "" {
			t.Fatalf("%s missing install path", name)
		}
	}
}

func TestShellInstallVerifyUninstall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallShell(context.Background(), db, notify, "zsh"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyShell(context.Background(), db, "zsh"); err != nil {
		t.Fatalf("fresh zsh hook must verify: %v", err)
	}
	info := ShellStatus(context.Background(), db, "zsh")
	if !info.Installed || info.InstallPath != filepath.Join(dir, ".zshrc") {
		t.Fatalf("bad shell status: %+v", info)
	}
	if err := UninstallShell(context.Background(), db, "zsh"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".zshrc")); string(got) != "" {
		t.Fatalf("expected hook removed, got %q", string(got))
	}
}
