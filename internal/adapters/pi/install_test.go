package pi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
)

func TestExtensionSourceMatchesPiContracts(t *testing.T) {
	src := extensionSource("/tmp/onibi-notify")
	for _, want := range []string{
		`pi.on("tool_call", async (event: any) => {`,
		`return { block: true, reason: decision.reason || "Denied by Onibi" }`,
		`const nextInput = parseUpdatedInput(decision.updated_input)`,
		`Object.assign(event.input, nextInput); // Pi does no post-mutation validation; validate before mutation`,
		`function parseUpdatedInput(value: string)`,
		`Array.isArray(parsed)`,
		`pi.on("session_shutdown", async (event: any) => emit("session_exited", event))`,
		`const ONIBI_INTEGRATION_VERSION = "` + common.IntegrationVersion + `"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("extension source missing %q", want)
		}
	}
}

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
	if err := os.WriteFile(types, []byte(`declare module "@earendil-works/pi-coding-agent" {
  export interface ExtensionAPI {
    on(name: string, handler: (event: any, ctx?: any) => any): void;
  }
}
declare module "node:child_process" {
  export function spawnSync(command: string, args?: readonly string[], options?: any): { status: number | null; stdout?: string };
}
`), 0o600); err != nil {
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
