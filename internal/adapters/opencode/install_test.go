package opencode

import (
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
)

func TestPluginSourceMatchesOpenCodeContracts(t *testing.T) {
	src := pluginSource("/tmp/onibi-notify")
	for _, want := range []string{
		`"tool.execute.before": async (input, output)`,
		`throw new Error(decision.reason || "Denied by Onibi")`,
		`output.args = parseUpdatedInput(decision.updated_input)`,
		`function parseUpdatedInput(value)`,
		`Array.isArray(parsed)`,
		`"session.idle": async (input) => emit("agent_done", input)`,
		`const ONIBI_INTEGRATION_VERSION = "` + common.IntegrationVersion + `"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("plugin source missing %q", want)
		}
	}
}

func TestTrustInstructionsMentionRestart(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	if !strings.Contains(got, "restart OpenCode") {
		t.Fatalf("instructions = %q", got)
	}
}
