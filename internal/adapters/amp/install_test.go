package amp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
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
		`const ONIBI_INTEGRATION_VERSION = "` + common.IntegrationVersion + `"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("plugin source missing %q", want)
		}
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
	if err := os.WriteFile(types, []byte(`declare module "@ampcode/plugin" {
  export interface PluginAPI {
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

func TestTrustInstructionsMentionReloadAndList(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	for _, want := range []string{"plugins: reload", "plugins: list"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instructions missing %q: %q", want, got)
		}
	}
}
