package devtool

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/termui"
)

// Explicit opt-in uses the real Go compiler and filesystem. The service home
// and installation prefix are temporary; no native apps or services are run.
func TestSourceLifecycleIntegration(t *testing.T) {
	if os.Getenv("ONIBI_SOURCE_SMOKE") != "1" {
		t.Skip("set ONIBI_SOURCE_SMOKE=1 to compile real source artifacts")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	tool := &Tool{Root: root, Prefix: filepath.Join(t.TempDir(), "bin with spaces"), UI: termui.New(&output)}
	opts := LifecycleOptions{Home: t.TempDir(), DryRun: true}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%v\n%s", err, output.String())
		}
	}
	check(tool.Install(t.Context(), false, opts))
	if _, err := os.Stat(tool.Prefix); !os.IsNotExist(err) {
		t.Fatal("preview created installation")
	}
	opts.DryRun = false
	check(tool.Install(t.Context(), false, opts))
	check(tool.Status(t.Context()))
	m, ok, err := tool.manifest()
	check(err)
	if !ok {
		t.Fatal("missing manifest")
	}
	version, err := tool.Runner.Run(t.Context(), root, nil, filepath.Join(tool.Prefix, "onibi"), []string{"version"}, nil)
	check(err)
	if strings.TrimSpace(version) != m.Version {
		t.Fatalf("artifact %q disagrees with manifest %q", version, m.Version)
	}
	check(tool.Install(t.Context(), true, opts))
	unrelated := filepath.Join(tool.Prefix, "unrelated")
	check(os.WriteFile(unrelated, []byte("keep"), 0600))
	state := filepath.Join(opts.Home, "preserved-state")
	check(os.WriteFile(state, []byte("keep"), 0600))
	opts.DryRun = true
	check(tool.Uninstall(t.Context(), opts))
	if _, err := os.Stat(filepath.Join(tool.Prefix, "onibi")); err != nil {
		t.Fatal("preview removed executable")
	}
	opts.DryRun = false
	check(tool.Uninstall(t.Context(), opts))
	for _, name := range append(append([]string{}, binaryNames...), manifestName) {
		if _, err := os.Stat(filepath.Join(tool.Prefix, name)); !os.IsNotExist(err) {
			t.Fatalf("managed file remains: %s", name)
		}
	}
	for _, path := range []string{unrelated, state} {
		raw, err := os.ReadFile(path)
		check(err)
		if string(raw) != "keep" {
			t.Fatal("unrelated data changed")
		}
	}
	t.Log("Real build, spaced-path install, update, status, previews and removal passed; state/unrelated files preserved.")
}
