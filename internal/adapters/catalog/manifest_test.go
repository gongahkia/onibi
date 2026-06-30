package catalog

import (
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/buildinfo"
)

func TestParseManifestRejectsHigherMinOnibiVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v0.3.0"
	t.Cleanup(func() { buildinfo.Version = old })
	_, err := ParseManifest([]byte(testManifest("future", "9.0.0")), "future.toml")
	if err == nil || !strings.Contains(err.Error(), "requires Onibi >= 9.0.0, running v0.3.0") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseManifestAcceptsDevMajorVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v2-dev"
	t.Cleanup(func() { buildinfo.Version = old })
	manifest, err := ParseManifest([]byte(testManifest("devok", "1.9.0")), "devok.toml")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "devok" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func testManifest(name, minVersion string) string {
	return strings.Join([]string{
		`name = "` + name + `"`,
		`version = "1.0.0"`,
		`kind = "agent"`,
		`cmd_pattern = { PreToolUse = "*" }`,
		`hook_install = ["printf ok"]`,
		`hook_uninstall = ["printf ok"]`,
		`min_onibi_version = "` + minVersion + `"`,
		``,
		`[risk_overrides]`,
		`Write = "high"`,
		``,
	}, "\n")
}
