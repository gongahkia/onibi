package pi

import (
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
)

func TestExtensionSourceMatchesPiContracts(t *testing.T) {
	src := extensionSource("/tmp/onibi-notify")
	for _, want := range []string{
		`pi.on("tool_call", async (event: any) => {`,
		`return { block: true, reason: decision.reason || "Denied by Onibi" }`,
		`Object.assign(event.input, parseUpdatedInput(decision.updated_input)); // pi does not re-validate mutated tool input`,
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

func TestTrustInstructionsMentionReload(t *testing.T) {
	got := strings.Join(TrustInstructions(), "\n")
	if !strings.Contains(got, "/reload") {
		t.Fatalf("instructions = %q", got)
	}
}
