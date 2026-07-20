package opencode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/adapters/denytest"
	"github.com/gongahkia/onibi/internal/store"
)

func TestPluginSourceMatchesOpenCodeContracts(t *testing.T) {
	src := pluginSource("/tmp/onibi-notify")
	for _, want := range []string{
		`"tool.execute.before": async (input, output)`,
		`throw new Error(decision.reason || "Denied by Onibi")`,
		`output.args = parseUpdatedInput(decision.updated_input)`,
		`function parseUpdatedInput(value)`,
		`Array.isArray(parsed)`,
		`async function emitEvent(input)`,
		`name === "session.deleted"`,
		`name === "session.idle"`,
		`"session.idle": async (input) => emit("agent_done", input)`,
		`const ONIBI_INTEGRATION_VERSION = "` + common.IntegrationVersion + `"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("plugin source missing %q", want)
		}
	}
}

func TestAdapterOpenCodeDenyBlocksTool(t *testing.T) {
	node := denytest.Node(t)
	dir := t.TempDir()
	notify := denytest.DenyNotify(t)
	target := denytest.Target(t, Agent)
	path := filepath.Join(dir, "onibi.mjs")
	if err := os.WriteFile(path, []byte(pluginSource(notify)), 0o600); err != nil {
		t.Fatal(err)
	}
	denytest.RunNodeScript(t, node, dir, `import fs from "node:fs/promises";
import { Onibi } from "./onibi.mjs";
const target = process.argv[2];
const plugin = await Onibi();
let blocked = false;
try {
  await plugin["tool.execute.before"]({ sessionID: "s1", cwd: process.cwd(), tool: "writeFile" }, { tool: "writeFile", args: { filePath: target, content: "x" } });
} catch {
  blocked = true;
}
if (!blocked) await fs.writeFile(target, "created\n");
`, target)
	denytest.AssertNotCreated(t, target)
}

func TestPluginApprovalDecisionMappings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		missing  bool
		blocked  bool
		content  string
	}{
		{name: "approve", response: `{"decision":"approve"}`, content: "original"},
		{name: "deny", response: `{"decision":"deny","reason":"denied"}`, blocked: true, content: "original"},
		{name: "expired", response: `{"decision":"expired","reason":"expired"}`, blocked: true, content: "original"},
		{name: "edited", response: `{"decision":"edited","updated_input":"{\"content\":\"edited\"}"}`, content: "edited"},
		{name: "timeout", response: `{"decision":"cancelled"}`, content: "original"},
		{name: "daemon_unavailable", missing: true, content: "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := denytest.Node(t)
			dir := t.TempDir()
			notify := filepath.Join(dir, "missing-onibi-notify")
			if !tc.missing {
				notify = decisionNotify(t, dir, tc.response)
			}
			if err := os.WriteFile(filepath.Join(dir, "onibi.mjs"), []byte(pluginSource(notify)), 0o600); err != nil {
				t.Fatal(err)
			}
			out := denytest.RunNodeScript(t, node, dir, `import { Onibi } from "./onibi.mjs";
const plugin = await Onibi();
const output = { tool: "writeFile", args: { content: "original" } };
let blocked = false;
try {
  await plugin["tool.execute.before"]({ sessionID: "s1", cwd: process.cwd(), tool: "writeFile" }, output);
} catch {
  blocked = true;
}
console.log(JSON.stringify({ blocked, args: output.args }));
`)
			if strings.Contains(out, `"blocked":true`) != tc.blocked {
				t.Fatalf("blocked=%t output=%s", tc.blocked, out)
			}
			if !strings.Contains(out, `"content":"`+tc.content+`"`) {
				t.Fatalf("content=%q output=%s", tc.content, out)
			}
		})
	}
}

func TestPluginMapsSessionExitAndTurnCompletion(t *testing.T) {
	node := denytest.Node(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "notify.log")
	t.Setenv("ONIBI_NOTIFY_RECORD", record)
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte(`#!/usr/bin/env node
import fs from "node:fs";
fs.appendFileSync(process.env.ONIBI_NOTIFY_RECORD, process.argv.slice(2).join(" ")+"\n");
`), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "onibi.mjs"), []byte(pluginSource(notify)), 0o600); err != nil {
		t.Fatal(err)
	}
	denytest.RunNodeScript(t, node, dir, `import { Onibi } from "./onibi.mjs";
const plugin = await Onibi();
await plugin.event({ type: "session.deleted", sessionID: "s1" });
await plugin["session.idle"]({ sessionID: "s1" });
`)
	body, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--type session_exited", "--type agent_done"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("notification missing %q: %s", want, body)
		}
	}
}

func TestExpectedAndObservedHooksReportPluginDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onibi.js")
	t.Setenv("ONIBI_OPENCODE_PLUGIN", path)
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
	observed, err := ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(pluginEvents) || len(observed) != len(pluginEvents) {
		t.Fatalf("expected=%+v observed=%+v", expected, observed)
	}
	for _, hook := range observed {
		if !hook.Managed || hook.Type != "plugin" || hook.Command != notify {
			t.Fatalf("observed hook=%+v", hook)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.Replace(string(body), `"tool.execute.after": async (input, output)`, `"tool.execute.after": async (input)`, 1))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err = ObservedHooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(pluginEvents)-1 {
		t.Fatalf("observed drift=%+v", observed)
	}
	if err := VerifyHash(context.Background(), db); err == nil {
		t.Fatal("tampered plugin verified")
	}
	if BackupPath(context.Background(), db) != "" {
		t.Fatal("unexpected backup for absent original plugin")
	}
}

func TestPluginInstallIsIdempotentAndUninstallKeepsOriginalBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onibi.js")
	notify := filepath.Join(dir, "onibi-notify")
	t.Setenv("ONIBI_OPENCODE_PLUGIN", path)
	if err := os.WriteFile(path, []byte("export const Original = () => {};\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	if err := Install(ctx, db, notify); err != nil {
		t.Fatal(err)
	}
	if err := Install(ctx, db, notify); err != nil {
		t.Fatal(err)
	}
	info := Status(ctx, db)
	if !info.Installed || !info.Managed || info.Tampered || info.MinimumProviderVersion != MinimumProviderVersion {
		t.Fatalf("status=%+v", info)
	}
	var backups int
	if err := db.SQL().QueryRowContext(ctx, "SELECT count(*) FROM hook_backups WHERE agent = ? AND path = ?", Agent, path).Scan(&backups); err != nil {
		t.Fatal(err)
	}
	if backups != 1 {
		t.Fatalf("backups after idempotent install=%d", backups)
	}
	if err := Uninstall(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plugin after uninstall err=%v", err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT count(*) FROM hook_backups WHERE agent = ? AND path = ?", Agent, path).Scan(&backups); err != nil {
		t.Fatal(err)
	}
	if backups != 1 {
		t.Fatalf("backups after uninstall=%d", backups)
	}
	backup := BackupPath(ctx, db)
	body, err := os.ReadFile(backup)
	if err != nil || string(body) != "export const Original = () => {};\n" {
		t.Fatalf("backup err=%v body=%q", err, body)
	}
}

func TestPluginPathScopes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := PluginPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".config", "opencode", "plugins", "onibi.js") {
		t.Fatalf("global path = %q", path)
	}

	cwd := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	cwd, err = os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_OPENCODE_SCOPE", "project")
	path, err = PluginPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(cwd, ".opencode", "plugins", "onibi.js") {
		t.Fatalf("project path = %q", path)
	}

	explicit := filepath.Join(t.TempDir(), "custom.js")
	t.Setenv("ONIBI_OPENCODE_PLUGIN", explicit)
	path, err = PluginPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != explicit {
		t.Fatalf("explicit path = %q", path)
	}
}

func TestGeneratedPluginFixtureParsesWithNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	path := filepath.Join(t.TempDir(), "onibi.mjs")
	if err := os.WriteFile(path, []byte(pluginSource("/tmp/onibi-notify")), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, "--check", path).CombinedOutput()
	if err != nil {
		t.Fatalf("node --check failed: %v\n%s", err, out)
	}
}

func TestTrustInstructionsMentionRestart(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	for _, want := range []string{"restart OpenCode", "ONIBI_OPENCODE_SCOPE=project", "ONIBI_OPENCODE_PLUGIN"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instructions missing %q: %q", want, got)
		}
	}
}

func decisionNotify(t *testing.T, dir, response string) string {
	t.Helper()
	path := filepath.Join(dir, "onibi-notify")
	body := "#!/bin/sh\nprintf '%s\\n' '" + response + "'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
