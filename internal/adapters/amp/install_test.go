package amp

import (
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

func TestTrustInstructionsMentionReloadAndList(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	for _, want := range []string{"plugins: reload", "plugins: list"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instructions missing %q: %q", want, got)
		}
	}
}
