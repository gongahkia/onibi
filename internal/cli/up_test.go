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
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
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

func TestUpAppliesProfileFlags(t *testing.T) {
	withDefaultState(t)
	cwd := t.TempDir()
	executeRoot(t, "profile", "add", "work", "--transport", "tailscale", "--agent", "sh", "--cwd", cwd, "--color", "never")
	oldWebPair := webPairRun
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		transport, _ := cmd.Flags().GetString("transport")
		agent, _ := cmd.Flags().GetString("agent")
		gotCWD, _ := cmd.Flags().GetString("cwd")
		if transport != "tailscale" || agent != "sh" || gotCWD != cwd {
			t.Fatalf("profile flags transport=%q agent=%q cwd=%q", transport, agent, gotCWD)
		}
		cmd.Println("profile pair stub")
		return nil
	}
	t.Cleanup(func() { webPairRun = oldWebPair })
	out, _ := executeRoot(t, "up", "work", "--color", "never")
	if !strings.Contains(out.String(), "profile pair stub") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestUpNoArgRecallsLastUsedProfile(t *testing.T) {
	withDefaultState(t)
	cwd := t.TempDir()
	executeRoot(t, "profile", "add", "work", "--transport", "tailscale", "--agent", "sh", "--cwd", cwd, "--use", "--color", "never")
	oldWebPair := webPairRun
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		transport, _ := cmd.Flags().GetString("transport")
		agent, _ := cmd.Flags().GetString("agent")
		gotCWD, _ := cmd.Flags().GetString("cwd")
		if transport != "tailscale" || agent != "sh" || gotCWD != cwd {
			t.Fatalf("profile flags transport=%q agent=%q cwd=%q", transport, agent, gotCWD)
		}
		cmd.Println("last profile stub")
		return nil
	}
	t.Cleanup(func() { webPairRun = oldWebPair })
	out, _ := executeRoot(t, "up", "--color", "never")
	if !strings.Contains(out.String(), "last profile stub") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestUpNoArgFallsBackWithoutLastUsedProfile(t *testing.T) {
	withDefaultState(t)
	executeRoot(t, "profile", "add", "work", "--transport", "tailscale", "--color", "never")
	oldWebPair := webPairRun
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		transport, _ := cmd.Flags().GetString("transport")
		if transport != "" {
			t.Fatalf("transport = %q", transport)
		}
		cmd.Println("fallback stub")
		return nil
	}
	t.Cleanup(func() { webPairRun = oldWebPair })
	out, _ := executeRoot(t, "up", "--color", "never")
	if !strings.Contains(out.String(), "fallback stub") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestUpDetachInstallsServiceAndPrintsPIDLog(t *testing.T) {
	paths := withDefaultState(t)
	oldInstall := installServiceRun
	oldPID := upServicePID
	installed := false
	installServiceRun = func(cmd *cobra.Command, _ []string) error {
		installed = true
		cmd.Println("service install stub")
		return nil
	}
	upServicePID = func(context.Context) (int, bool, error) {
		return 4242, true, nil
	}
	t.Cleanup(func() {
		installServiceRun = oldInstall
		upServicePID = oldPID
	})
	out, _ := executeRoot(t, "up", "--detach", "--color", "never")
	for _, want := range []string{"service install stub", "PID: 4242", filepath.Join(paths.LogDir, "onibi.log")} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("detach output missing %q: %q", want, out.String())
		}
	}
	if !installed {
		t.Fatal("install service was not called")
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

func TestApplyWireGuardListenAddrUsesDetectedHost(t *testing.T) {
	old := wireGuardBindHost
	wireGuardBindHost = func(context.Context) (string, error) { return "10.8.0.2", nil }
	t.Cleanup(func() { wireGuardBindHost = old })
	cfg := config.Default()
	cfg.Transport.Mode = "wireguard"
	cfg.Web.ListenAddr = ":9443"
	if err := applyWireGuardListenAddr(context.Background(), &cfg, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if cfg.Web.ListenAddr != "10.8.0.2:9443" {
		t.Fatalf("listen_addr = %q", cfg.Web.ListenAddr)
	}
}

func TestApplyWireGuardListenAddrFormatsIPv6(t *testing.T) {
	old := wireGuardBindHost
	wireGuardBindHost = func(context.Context) (string, error) { return "fd00::2", nil }
	t.Cleanup(func() { wireGuardBindHost = old })
	cfg := config.Default()
	cfg.Transport.Mode = "wireguard"
	cfg.Web.ListenAddr = "0.0.0.0:9443"
	if err := applyWireGuardListenAddr(context.Background(), &cfg, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if cfg.Web.ListenAddr != "[fd00::2]:9443" {
		t.Fatalf("listen_addr = %q", cfg.Web.ListenAddr)
	}
}

func TestApplyZeroTierListenAddrUsesDetectedHost(t *testing.T) {
	old := zeroTierBindHost
	zeroTierBindHost = func(context.Context) (string, error) { return "10.147.20.4", nil }
	t.Cleanup(func() { zeroTierBindHost = old })
	cfg := config.Default()
	cfg.Transport.Mode = "zerotier"
	cfg.Web.ListenAddr = ":9443"
	if err := applyZeroTierListenAddr(context.Background(), &cfg, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if cfg.Web.ListenAddr != "10.147.20.4:9443" {
		t.Fatalf("listen_addr = %q", cfg.Web.ListenAddr)
	}
}

func TestApplyZeroTierListenAddrFormatsIPv6(t *testing.T) {
	old := zeroTierBindHost
	zeroTierBindHost = func(context.Context) (string, error) { return "fd00:147::4", nil }
	t.Cleanup(func() { zeroTierBindHost = old })
	cfg := config.Default()
	cfg.Transport.Mode = "zerotier"
	cfg.Web.ListenAddr = "0.0.0.0:9443"
	if err := applyZeroTierListenAddr(context.Background(), &cfg, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if cfg.Web.ListenAddr != "[fd00:147::4]:9443" {
		t.Fatalf("listen_addr = %q", cfg.Web.ListenAddr)
	}
}

func TestPrivatePairTransportRejectsEndpointChange(t *testing.T) {
	for _, mode := range []webtransport.Mode{webtransport.ModeWireGuard, webtransport.ModeZeroTier} {
		err := validatePrivatePairTransport("10.147.20.4:9443", webtransport.Resolved{Mode: mode, BaseURL: "https://10.147.20.5:9443"})
		if err == nil || !strings.Contains(err.Error(), "endpoint changed") {
			t.Fatalf("mode=%s err=%v", mode, err)
		}
		if err := validatePrivatePairTransport("[fd00:147::4]:9443", webtransport.Resolved{Mode: mode, BaseURL: "https://[fd00:147::4]:9443"}); err != nil {
			t.Fatalf("mode=%s err=%v", mode, err)
		}
	}
}

func TestListenCertHostsAndWebHealthURLUseSelectedListener(t *testing.T) {
	if got := listenCertHosts("10.147.20.4:9443"); len(got) != 1 || got[0] != "10.147.20.4" {
		t.Fatalf("cert hosts=%q", got)
	}
	for _, tc := range []struct {
		listen string
		want   string
	}{
		{listen: "10.147.20.4:9443", want: "https://10.147.20.4:9443/healthz"},
		{listen: "[fd00:147::4]:9443", want: "https://[fd00:147::4]:9443/healthz"},
		{listen: ":9443", want: "https://127.0.0.1:9443/healthz"},
	} {
		got, err := webHealthURL(tc.listen)
		if err != nil || got != tc.want {
			t.Fatalf("listen=%q url=%q err=%v", tc.listen, got, err)
		}
	}
}

func TestPublicRelayModesForceRelayE2E(t *testing.T) {
	for _, mode := range []webtransport.Mode{webtransport.ModeCloudflareQuick, webtransport.ModeNgrok} {
		if !webtransport.IsRelayMode(string(mode)) {
			t.Fatalf("%s did not require relay e2e", mode)
		}
	}
}

func TestUpHelpDocumentsPublicRelayE2ERequirement(t *testing.T) {
	out, _, err := executeRootAllowError(t, "up", "--help", "--color", "never")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "E2E is required for public relay transport (Cloudflare and ngrok)") {
		t.Fatalf("help missing E2E requirement:\n%s", out.String())
	}
}

func TestRuntimeTransportHealthIncludesNgrok(t *testing.T) {
	for _, mode := range []webtransport.Mode{webtransport.ModeNgrok, webtransport.ModeCloudflareQuick} {
		if !requiresRuntimeTransportHealth(mode) {
			t.Fatalf("%s runtime health disabled", mode)
		}
	}
}

func TestUnsafeCloudflareNoE2EFlagRemoved(t *testing.T) {
	flag := "--unsafe-cloudflare-" + "no-e2e"
	out, errOut, err := executeRootAllowError(t, "up", "--transport=cloudflare-quick", flag, "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: "+flag) {
		t.Fatalf("expected unknown flag, got err=%v out=%s err=%s", err, out.String(), errOut.String())
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	t.Setenv("ONIBI_STORE_KEY_BACKEND", "dotenv")
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
