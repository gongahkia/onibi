package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
	"github.com/gongahkia/onibi/internal/workspace"
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
	old := newTransportProviders
	fake := &fakePairTransport{url: "https://dev.tail.ts.net/"}
	newTransportProviders = func() webtransport.ProviderFactory {
		return webtransport.ProviderFactory{Tailscale: func() webtransport.Provider { return fake }}
	}
	t.Cleanup(func() { newTransportProviders = old })

	pt, err := resolvePairTransport(context.Background(), "auto", 8443, []string{"192.0.2.10"}, "host.local", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if pt.Mode != webtransport.ModeTailscale {
		t.Fatalf("mode = %q", pt.Mode)
	}
	if got := pt.URLs("tok"); len(got) != 1 || got[0] != "https://dev.tail.ts.net/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
	if fake.enablePort != 8443 {
		t.Fatalf("enable port = %d", fake.enablePort)
	}
	if err := pt.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.disabled {
		t.Fatal("disable not called")
	}
}

func TestResolvePairTransportAutoFallsBackToLAN(t *testing.T) {
	old := newTransportProviders
	newTransportProviders = func() webtransport.ProviderFactory {
		return webtransport.ProviderFactory{Tailscale: func() webtransport.Provider {
			return &fakePairTransport{enableErr: errors.New("no tailscale")}
		}}
	}
	t.Cleanup(func() { newTransportProviders = old })

	pt, err := resolvePairTransport(context.Background(), "auto", 8443, []string{"192.0.2.10"}, "host.local", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if pt.Mode != webtransport.ModeLAN {
		t.Fatalf("mode = %q", pt.Mode)
	}
	got := pt.URLs("tok")
	if len(got) == 0 || got[0] != "https://192.0.2.10:8443/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestResolveUpWorkspaceLoadsProjectConfig(t *testing.T) {
	db := openUpTestDB(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".onibi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, workspace.ProjectRelPath), []byte(`
schema_version = 1
name = "alpha"

[transports]
default = "tailscale"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "cmd")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveUpWorkspace(context.Background(), db, nested, false)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Found || resolved.Source != "project" || resolved.Name != "alpha" || resolved.Root != root || resolved.ShellCWD != nested || resolved.DefaultTransport != "tailscale" {
		t.Fatalf("resolved = %#v", resolved)
	}
	cfg := config.Default()
	cmd := upCmd()
	applied, err := applyWorkspaceTransport(cmd, &cfg, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !applied || cfg.Transport.Mode != "tailscale" {
		t.Fatalf("applied=%v transport=%q", applied, cfg.Transport.Mode)
	}
}

func TestResolveUpWorkspaceFallsBackToDefaultWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	db := openUpTestDB(t)
	ctx := context.Background()
	wsPath := filepath.Join(t.TempDir(), "default-workspace")
	if err := os.MkdirAll(wsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	wsStore, err := workspace.NewDBStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsStore.Upsert(ctx, workspace.DBEntry{Name: "default", Path: wsPath, LastSeen: time.Unix(2, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.SetDefaultName(ctx, db, "default"); err != nil {
		t.Fatal(err)
	}
	indexDir, err := workspace.DefaultIndexDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.SaveIndexEntry(indexDir, workspace.IndexEntry{Name: "default", Path: wsPath, LastSeen: time.Unix(2, 0).UTC(), DefaultTransport: "tailscale"}); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveUpWorkspace(ctx, db, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Found || resolved.Source != "default" || resolved.Name != "default" || resolved.ShellCWD != wsPath || resolved.DefaultTransport != "tailscale" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveUpWorkspaceDoesNotFallbackWhenCWDExplicit(t *testing.T) {
	db := openUpTestDB(t)
	ctx := context.Background()
	wsPath := filepath.Join(t.TempDir(), "default-workspace")
	if err := os.MkdirAll(wsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	wsStore, err := workspace.NewDBStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsStore.Upsert(ctx, workspace.DBEntry{Name: "default", Path: wsPath, LastSeen: time.Unix(2, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.SetDefaultName(ctx, db, "default"); err != nil {
		t.Fatal(err)
	}
	start := t.TempDir()
	resolved, err := resolveUpWorkspace(ctx, db, start, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Found || resolved.ShellCWD != start {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestCloudflareQuickForcesRelayE2E(t *testing.T) {
	if !webtransport.IsRelayMode("cloudflare-quick") {
		t.Fatal("cloudflare-quick did not require relay e2e")
	}
}

func TestRelayPairURLFragmentKeepsKeyOutOfRequestPath(t *testing.T) {
	got := appendURLFragment("https://fast-demo.trycloudflare.com/pair/tok", "k=abc123")
	if got != "https://fast-demo.trycloudflare.com/pair/tok#k=abc123" {
		t.Fatalf("url = %q", got)
	}
	if strings.Contains(strings.Split(got, "#")[0], "abc123") {
		t.Fatalf("key leaked before fragment: %q", got)
	}
}

func TestRedactPairURLDropsRelayKeyFragment(t *testing.T) {
	got := redactPairURL("https://fast-demo.trycloudflare.com/pair/tok#k=abc123")
	if got != "https://fast-demo.trycloudflare.com/pair/<redacted>" {
		t.Fatalf("redacted = %q", got)
	}
}

func openUpTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
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

func (f *fakePairTransport) Check(context.Context) error {
	return nil
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
