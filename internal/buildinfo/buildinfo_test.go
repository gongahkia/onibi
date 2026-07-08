package buildinfo

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestReleasePublicKeyDecodesBase64(t *testing.T) {
	old := ReleasePublicKeyB64
	t.Cleanup(func() { ReleasePublicKeyB64 = old })
	ReleasePublicKeyB64 = base64.StdEncoding.EncodeToString([]byte("  key\n"))
	if got := ReleasePublicKey(); got != "key" {
		t.Fatalf("ReleasePublicKey() = %q", got)
	}
}

func TestReleasePublicKeyIgnoresInvalidBase64(t *testing.T) {
	old := ReleasePublicKeyB64
	t.Cleanup(func() { ReleasePublicKeyB64 = old })
	ReleasePublicKeyB64 = "%"
	if got := ReleasePublicKey(); got != "" {
		t.Fatalf("ReleasePublicKey() = %q", got)
	}
}

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
