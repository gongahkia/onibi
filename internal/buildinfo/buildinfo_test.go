package buildinfo

import (
	"os"
	"strings"
	"testing"
)

func TestLDFlagInjectionTargets(t *testing.T) {
	for _, path := range []string{"../../Makefile", "../../.goreleaser.yaml"} {
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
