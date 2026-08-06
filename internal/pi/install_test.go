package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionPathAndInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onibi.ts")
	t.Setenv("ONIBI_PI_EXTENSION", path)
	notify := filepath.Join(t.TempDir(), "onibi-notify")
	if err := os.WriteFile(notify, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Install(context.Background(), notify)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("path=%q", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pi.on(\"tool_call\"", "approval_request", "onibi.pi.v1", "timeout: 305_000", "decision.decision === \"approve\"", notify} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %q", want)
		}
	}
}
