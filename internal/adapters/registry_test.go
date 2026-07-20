package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/catalog"
	"github.com/gongahkia/onibi/internal/store"
)

func TestManifestRegistryRegisterListGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Manifest{
		Name:            "zeta",
		Version:         "1.0.0",
		Kind:            catalog.KindAgent,
		CmdPattern:      map[string]string{"PreToolUse": "*"},
		HookInstall:     []string{"onibi install-hooks --agent zeta"},
		HookUninstall:   []string{"onibi uninstall --agent zeta --yes"},
		RiskOverrides:   map[string]string{"Write": "high"},
		MinOnibiVersion: "0.3.0",
		Adapter:         Adapter{Name: "zeta"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Manifest{Name: "alpha", Version: "1.0.0", Kind: catalog.KindAgent, Adapter: Adapter{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Manifest{Name: "alpha", Version: "1.0.1", Kind: catalog.KindAgent}); err == nil {
		t.Fatal("duplicate adapter registered")
	}
	list := r.List()
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("list = %#v", list)
	}
	list[1].CmdPattern["PreToolUse"] = "Read"
	got, err := r.Get("zeta")
	if err != nil {
		t.Fatal(err)
	}
	if got.CmdPattern["PreToolUse"] != "*" {
		t.Fatalf("registry returned mutable manifest: %#v", got.CmdPattern)
	}
}

func TestBuiltinAdaptersExposeManifests(t *testing.T) {
	want := []string{"amp", "claude", "codex", "copilot", "gemini", "goose", "opencode", "pi"}
	got := List()
	if len(got) != len(want) {
		t.Fatalf("manifest count = %d", len(got))
	}
	for i, name := range want {
		if got[i].Name != name || got[i].Kind != catalog.KindAgent || got[i].Adapter.Install == nil {
			t.Fatalf("manifest[%d] = %#v", i, got[i])
		}
	}
}

func TestDetectedNamesForBuiltinAdapters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearDetectEnv(t)
	for _, dir := range []string{
		".claude",
		filepath.Join(".config", "codex"),
		".copilot",
		".gemini",
		".goose",
		filepath.Join(".config", "opencode"),
		filepath.Join(".config", "amp"),
		".pi",
	} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got := setOf(DetectedNames())
	for _, want := range []string{"amp", "claude", "codex", "copilot", "gemini", "goose", "opencode", "pi"} {
		if !got[want] {
			t.Fatalf("missing detected adapter %q in %v", want, got)
		}
	}
}

func TestRegistryAdaptersInstallAndVerify(t *testing.T) {
	db, notify := adapterRegistryFixture(t)

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

func TestRegistryAdapterVerifyPathsForParityAgents(t *testing.T) {
	db, notify := adapterRegistryFixture(t)
	for _, name := range []string{"gemini", "copilot", "goose", "opencode", "amp", "pi"} {
		t.Run(name, func(t *testing.T) {
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
		})
	}
}

func TestStatusReportsAdoptableWhenHashMissing(t *testing.T) {
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
	t.Setenv("ONIBI_CODEX_HOOKS", filepath.Join(dir, "codex-hooks.json"))
	a, _ := Get("codex")
	if err := a.Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), `DELETE FROM hooks WHERE agent = 'codex'`); err != nil {
		t.Fatal(err)
	}
	info := a.Status(context.Background(), db)
	if !info.Installed || !info.Managed || !info.Adoptable || info.HashRecorded || info.Next == "" {
		t.Fatalf("bad status: %+v", info)
	}
}

func adapterRegistryFixture(t *testing.T) (*store.DB, string) {
	t.Helper()
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
	return db, notify
}

func clearDetectEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CLAUDE_CONFIG_DIR",
		"ONIBI_CODEX_HOOKS",
		"ONIBI_COPILOT_HOOK",
		"COPILOT_HOME",
		"ONIBI_GEMINI_SETTINGS",
		"ONIBI_GOOSE_HOOKS",
		"ONIBI_OPENCODE_PLUGIN",
		"ONIBI_OPENCODE_SCOPE",
		"ONIBI_AMP_PLUGIN",
		"ONIBI_PI_EXTENSION",
		"ONIBI_PI_SCOPE",
		"PI_CODING_AGENT_DIR",
	} {
		t.Setenv(key, "")
	}
}

func setOf(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}
