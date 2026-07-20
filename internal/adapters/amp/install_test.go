package amp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/adapters/denytest"
	"github.com/gongahkia/onibi/internal/store"
)

func TestPluginSourceMatchesAmpToolCallContracts(t *testing.T) {
	src := pluginSource("/tmp/onibi-notify")
	for _, want := range []string{
		`amp.on("tool.call", async (event: any) => {`,
		`return await approval(event)`,
		`action: "reject-and-continue"`,
		`action: "modify", input: parseUpdatedInput(decision.updated_input)`,
		`return { action: "allow" }`,
		`function parseUpdatedInput(value: string)`,
		`Array.isArray(parsed)`,
		`const bun = (globalThis as any).Bun;`,
		`const p = bun.spawn([ONIBI_NOTIFY, ...args]`,
		`provider_session_id: event?.thread?.id`,
		`const ONIBI_INTEGRATION_VERSION = "` + common.IntegrationVersion + `"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("plugin source missing %q", want)
		}
	}
}

func TestAdapterAmpDenyBlocksTool(t *testing.T) {
	node := denytest.Node(t)
	tsc := denytest.TSC(t)
	dir := t.TempDir()
	notify := denytest.DenyNotify(t)
	target := denytest.Target(t, Agent)
	denytest.CompileTSModule(t, tsc, dir, "onibi.ts", pluginSource(notify), `declare module "@ampcode/plugin" {
  export interface PluginAPI {
    on(name: string, handler: (event: any, ctx?: any) => any): void;
  }
}
declare module "node:child_process" {
  export function spawnSync(command: string, args?: readonly string[], options?: any): { status: number | null; stdout?: string };
}
`)
	denytest.RunNodeScript(t, node, dir, `import fs from "node:fs/promises";
import plugin from "./out/onibi.js";
const target = process.argv[2];
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
const result = await handlers.get("tool.call")({ sessionId: "s1", cwd: process.cwd(), toolName: "writeFile", input: { filePath: target, content: "x" } });
if (result?.action !== "reject-and-continue") await fs.writeFile(target, "created\n");
`, target)
	denytest.AssertNotCreated(t, target)
}

func TestPluginApprovalDecisionMappings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		missing  bool
		action   string
		content  string
	}{
		{name: "approve", response: `{"decision":"approve"}`, action: "allow", content: "original"},
		{name: "deny", response: `{"decision":"deny","reason":"denied"}`, action: "reject-and-continue", content: "original"},
		{name: "expired", response: `{"decision":"expired","reason":"expired"}`, action: "reject-and-continue", content: "original"},
		{name: "edited", response: `{"decision":"edited","updated_input":"{\"content\":\"edited\"}"}`, action: "modify", content: "edited"},
		{name: "timeout", response: `{"decision":"cancelled"}`, action: "allow", content: "original"},
		{name: "daemon_unavailable", missing: true, action: "allow", content: "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := denytest.Node(t)
			tsc := denytest.TSC(t)
			dir := t.TempDir()
			notify := filepath.Join(dir, "missing-onibi-notify")
			if !tc.missing {
				notify = decisionNotify(t, dir, tc.response)
			}
			denytest.CompileTSModule(t, tsc, dir, "onibi.ts", pluginSource(notify), ampPluginTypes)
			out := denytest.RunNodeScript(t, node, dir, `import plugin from "./out/onibi.js";
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
const input = { content: "original" };
const result = await handlers.get("tool.call")({ thread: { id: "T-1" }, tool: "writeFile", input });
console.log(JSON.stringify({ result, input: result?.input ?? input }));
`)
			if !strings.Contains(out, `"action":"`+tc.action+`"`) || !strings.Contains(out, `"content":"`+tc.content+`"`) {
				t.Fatalf("result=%s", out)
			}
		})
	}
}

func TestPluginForwardsThreadIDsAndLifecycle(t *testing.T) {
	node := denytest.Node(t)
	tsc := denytest.TSC(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "notify.log")
	t.Setenv("ONIBI_AMP_RECORD", record)
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte(`#!/bin/sh
printf '%s\t' "$*" >> "$ONIBI_AMP_RECORD"
cat >> "$ONIBI_AMP_RECORD"
printf '\n' >> "$ONIBI_AMP_RECORD"
case " $* " in
  *" --type approval_request "*) printf '{"decision":"approve"}\n' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	denytest.CompileTSModule(t, tsc, dir, "onibi.ts", pluginSource(notify), ampPluginTypes)
	denytest.RunNodeScript(t, node, dir, `import plugin from "./out/onibi.js";
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
await handlers.get("session.start")({ thread: { id: "T-old" } });
await handlers.get("tool.call")({ thread: { id: "T-new" }, tool: "writeFile", input: { content: "x" } });
await handlers.get("agent.end")({ thread: { id: "T-new" } });
`)
	body, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--type approval_request",
		"--type agent_done",
		`"session_id":"T-new"`,
		`"provider_session_id":"T-new"`,
		`"id":"T-old"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("notification missing %q: %s", want, body)
		}
	}
}

func TestExpectedAndObservedHooksReportPluginDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onibi.ts")
	t.Setenv("ONIBI_AMP_PLUGIN", path)
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
	body = []byte(strings.Replace(string(body), `amp.on("tool.result", async (event: any) => emit(`, `amp.on("tool.result", async (event) => emit(`, 1))
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

func TestGeneratedPluginTypeChecksWithFixture(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "onibi.ts")
	if err := os.WriteFile(src, []byte(pluginSource("/tmp/onibi-notify")), 0o600); err != nil {
		t.Fatal(err)
	}
	types := filepath.Join(dir, "types.d.ts")
	if err := os.WriteFile(types, []byte(ampPluginTypes), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tsc, "--noEmit", "--target", "ES2022", "--module", "NodeNext", "--moduleResolution", "NodeNext", "--skipLibCheck", src, types).CombinedOutput()
	if err != nil {
		t.Fatalf("tsc failed: %v\n%s", err, out)
	}
}

const ampPluginTypes = `declare module "@ampcode/plugin" {
  export interface PluginAPI {
    on(name: string, handler: (event: any, ctx?: any) => any): void;
  }
}
declare module "node:child_process" {
  export function spawnSync(command: string, args?: readonly string[], options?: any): { status: number | null; stdout?: string };
}
`

func decisionNotify(t *testing.T, dir, response string) string {
	t.Helper()
	path := filepath.Join(dir, "onibi-notify")
	body := "#!/bin/sh\nprintf '%s\\n' '" + response + "'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTrustInstructionsMentionReloadAndList(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	for _, want := range []string{"plugins: reload", "plugins: list"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instructions missing %q: %q", want, got)
		}
	}
}
