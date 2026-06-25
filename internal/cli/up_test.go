package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func TestUpStartsWebPair(t *testing.T) {
	withDefaultState(t)
	oldWebPair := webPairRun
	oldInstall := installServiceRun
	webPairCalled := false
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		webPairCalled = true
		cmd.Println("pair stub")
		return nil
	}
	installServiceRun = func(*cobra.Command, []string) error {
		t.Fatal("install service should not run")
		return nil
	}
	t.Cleanup(func() {
		webPairRun = oldWebPair
		installServiceRun = oldInstall
	})

	var out bytes.Buffer
	cmd := upCmd()
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !webPairCalled {
		t.Fatal("web pair not called")
	}
	if !strings.Contains(out.String(), "pair stub") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestWebPairURLsIncludesFallbacks(t *testing.T) {
	got := webPairURLs("tok", 8443, []string{"192.168.1.31", "host.local", ""}, "host.local")
	want := []string{
		"https://192.168.1.31:8443/pair/tok",
		"https://host.local:8443/pair/tok",
	}
	if len(got) != len(want) {
		t.Fatalf("urls = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("urls = %#v", got)
		}
	}
}

func withDefaultState(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return paths
}
