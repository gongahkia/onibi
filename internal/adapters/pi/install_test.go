package pi

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/adapters/denytest"
)

func TestExtensionSourceMatchesPiContracts(t *testing.T) {
	src := extensionSource("/tmp/onibi-notify")
	for _, want := range []string{
		`pi.on("tool_call", async (event: any, ctx: any) => {`,
		`return { block: true, reason: decision.reason || "Denied by Onibi" }`,
		`const nextInput = parseUpdatedInput(decision.updated_input)`,
		`Object.assign(event.input, nextInput); // Pi does no post-mutation validation; validate before mutation`,
		`function parseUpdatedInput(value: string)`,
		`Array.isArray(parsed)`,
		`Invalid Onibi approval response`,
		`Invalid Onibi edited input`,
		`const providerSessionID = ctx?.sessionManager?.getSessionId?.()`,
		`provider_session_id: ctx?.sessionManager?.getSessionId?.()`,
		`pi.on("session_start", async (event: any, ctx: any) => emit("agent_message", event, ctx))`,
		`pi.on("agent_end", async (event: any, ctx: any) => emit("agent_done", event, ctx))`,
		`pi.on("session_shutdown", async (event: any, ctx: any) => emit("session_exited", event, ctx))`,
		`const ONIBI_INTEGRATION_VERSION = "` + common.IntegrationVersion + `"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("extension source missing %q", want)
		}
	}
}

func TestExtensionEmitsNativePiSessionID(t *testing.T) {
	node := denytest.Node(t)
	tsc := denytest.TSC(t)
	dir := t.TempDir()
	capture := filepath.Join(dir, "payload.json")
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\ncat > \"$ONIBI_CAPTURE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_CAPTURE", capture)
	denytest.CompileTSModule(t, tsc, dir, "onibi.ts", extensionSource(notify), piExtensionTypes)
	denytest.RunNodeScript(t, node, dir, `import plugin from "./out/onibi.js";
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
await handlers.get("agent_end")({}, { cwd: "/tmp/repo", sessionManager: { getSessionId: () => "pi-native-session" } });
`)
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["provider_session_id"] != "pi-native-session" || payload["cwd"] != "/tmp/repo" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestAdapterPiEditReplacesToolInput(t *testing.T) {
	node := denytest.Node(t)
	tsc := denytest.TSC(t)
	dir := t.TempDir()
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nprintf '%s\\n' '{\"decision\":\"edited\",\"updated_input\":\"{\\\"content\\\":\\\"safe\\\"}\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	denytest.CompileTSModule(t, tsc, dir, "onibi.ts", extensionSource(notify), piExtensionTypes)
	denytest.RunNodeScript(t, node, dir, `import plugin from "./out/onibi.js";
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
const event = { sessionId: "s1", cwd: process.cwd(), toolName: "writeFile", input: { content: "unsafe" } };
const result = await handlers.get("tool_call")(event);
if (result?.block === true || event.input.content !== "safe") throw new Error(JSON.stringify({ result, input: event.input }));
`)
}

func TestAdapterPiInvalidApprovalResponseBlocksTool(t *testing.T) {
	node := denytest.Node(t)
	tsc := denytest.TSC(t)
	dir := t.TempDir()
	notify := filepath.Join(dir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nprintf 'not-json\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	denytest.CompileTSModule(t, tsc, dir, "onibi.ts", extensionSource(notify), piExtensionTypes)
	denytest.RunNodeScript(t, node, dir, `import plugin from "./out/onibi.js";
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
const result = await handlers.get("tool_call")({ sessionId: "s1", cwd: process.cwd(), toolName: "writeFile", input: { content: "unsafe" } });
if (result?.block !== true) throw new Error(JSON.stringify(result));
`)
}

func TestAdapterPiDenyBlocksTool(t *testing.T) {
	node := denytest.Node(t)
	tsc := denytest.TSC(t)
	dir := t.TempDir()
	notify := denytest.DenyNotify(t)
	target := denytest.Target(t, Agent)
	denytest.CompileTSModule(t, tsc, dir, "onibi.ts", extensionSource(notify), piExtensionTypes)
	denytest.RunNodeScript(t, node, dir, `import fs from "node:fs/promises";
import plugin from "./out/onibi.js";
const target = process.argv[2];
const handlers = new Map();
plugin({ on(name, handler) { handlers.set(name, handler); } });
const result = await handlers.get("tool_call")({ sessionId: "s1", cwd: process.cwd(), toolName: "writeFile", input: { filePath: target, content: "x" } });
if (result?.block !== true) await fs.writeFile(target, "created\n");
`, target)
	denytest.AssertNotCreated(t, target)
}

const piExtensionTypes = `declare module "@earendil-works/pi-coding-agent" {
  export interface ExtensionAPI {
    on(name: string, handler: (event: any, ctx?: any) => any): void;
  }
}
declare module "node:child_process" {
  export function spawnSync(command: string, args?: readonly string[], options?: any): { status: number | null; stdout?: string };
}
`

func TestExtensionPathScopes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := ExtensionPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".pi", "agent", "extensions", "onibi.ts") {
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
	t.Setenv("ONIBI_PI_SCOPE", "project")
	path, err = ExtensionPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(cwd, ".pi", "extensions", "onibi.ts") {
		t.Fatalf("project path = %q", path)
	}

	explicit := filepath.Join(t.TempDir(), "custom.ts")
	t.Setenv("ONIBI_PI_EXTENSION", explicit)
	path, err = ExtensionPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != explicit {
		t.Fatalf("explicit path = %q", path)
	}
}

func TestGeneratedExtensionTypeChecksWithFixture(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "onibi.ts")
	if err := os.WriteFile(src, []byte(extensionSource("/tmp/onibi-notify")), 0o600); err != nil {
		t.Fatal(err)
	}
	types := filepath.Join(dir, "types.d.ts")
	if err := os.WriteFile(types, []byte(piExtensionTypes), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tsc, "--noEmit", "--target", "ES2022", "--module", "NodeNext", "--moduleResolution", "NodeNext", "--skipLibCheck", src, types).CombinedOutput()
	if err != nil {
		t.Fatalf("tsc failed: %v\n%s", err, out)
	}
}

func TestTrustInstructionsMentionReload(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	for _, want := range []string{"/reload", "ONIBI_PI_SCOPE=project", "ONIBI_PI_EXTENSION"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instructions missing %q: %q", want, got)
		}
	}
}
