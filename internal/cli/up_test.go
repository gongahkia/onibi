package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
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

func TestResolvePairTransportAutoUsesTailscale(t *testing.T) {
	old := newTailscalePairTransport
	fake := &fakePairTransport{url: "https://dev.tail.ts.net/"}
	newTailscalePairTransport = func() tailscalePairTransport { return fake }
	t.Cleanup(func() { newTailscalePairTransport = old })

	pt, err := resolvePairTransport(context.Background(), "auto", 8443, []string{"192.0.2.10"}, "host.local", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if pt.mode != "tailscale" {
		t.Fatalf("mode = %q", pt.mode)
	}
	if got := pt.URLs("tok"); len(got) != 1 || got[0] != "https://dev.tail.ts.net/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
	if fake.enablePort != 8443 {
		t.Fatalf("enable port = %d", fake.enablePort)
	}
	if err := pt.cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.disabled {
		t.Fatal("disable not called")
	}
}

func TestResolvePairTransportAutoFallsBackToLAN(t *testing.T) {
	old := newTailscalePairTransport
	newTailscalePairTransport = func() tailscalePairTransport {
		return &fakePairTransport{enableErr: errors.New("no tailscale")}
	}
	t.Cleanup(func() { newTailscalePairTransport = old })

	pt, err := resolvePairTransport(context.Background(), "auto", 8443, []string{"192.0.2.10"}, "host.local", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if pt.mode != "lan" {
		t.Fatalf("mode = %q", pt.mode)
	}
	got := pt.URLs("tok")
	if len(got) == 0 || got[0] != "https://192.0.2.10:8443/pair/tok" {
		t.Fatalf("urls = %#v", got)
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakePairTransport struct {
	url        string
	enableErr  error
	enablePort int
	disabled   bool
}

func (f *fakePairTransport) Enable(_ context.Context, port int) error {
	f.enablePort = port
	return f.enableErr
}

func (f *fakePairTransport) URL(context.Context) (string, error) {
	return f.url, nil
}

func (f *fakePairTransport) Disable(context.Context) error {
	f.disabled = true
	return nil
}
