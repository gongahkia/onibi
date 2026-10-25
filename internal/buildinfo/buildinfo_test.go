package buildinfo

import (
	"os"
	"strings"
	"testing"
)

// Source-build injection is checked against a real installed executable by
// devtool.TestSourceLifecycleIntegration. Keep the release config check here.
func TestReleaseLDFlagInjectionTargets(t *testing.T) {
	for _, path := range []string{"../../.goreleaser.yaml"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		for _, symbol := range []string{"Version", "Commit", "Date"} {
			want := "-X github.com/gongahkia/onibi/internal/buildinfo." + symbol + "="
			if !strings.Contains(src, want) {
				t.Fatalf("%s missing ldflag target %q", path, want)
			}
		}
	}
}
