package opencode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/adapters/denytest"
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
