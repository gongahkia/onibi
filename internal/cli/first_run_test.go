package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/capability"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func TestFirstRunHappyPathInstallsDetectedHooksAndStartsUp(t *testing.T) {
	paths := withDefaultState(t)
	home := firstRunHome(t)
	firstRunNotifyFixture(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldWebPair := webPairRun
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		transport, _ := cmd.Flags().GetString("transport")
		if transport != "tailscale" {
			t.Fatalf("transport = %q", transport)
		}
		cmd.Println("first-run pair stub")
		return nil
	}
	t.Cleanup(func() { webPairRun = oldWebPair })

	out, _ := executeRootInput(t, "all\n2\n", "up", "--first-run", "--color", "never", "--no-logo")
	got := out.String()
	for _, want := range []string{"First run", "Detected hooks", "Installed claude hooks", "Transport tailscale", "first-run pair stub"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport.Mode != "tailscale" {
		t.Fatalf("saved transport = %q", cfg.Transport.Mode)
	}
}

func TestFirstRunNoDetectedHooksSkipsInstallAndStartsUp(t *testing.T) {
	paths := withDefaultState(t)
	home := firstRunHome(t)
	oldWebPair := webPairRun
	oldLocate := locateNotifyBinary
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		transport, _ := cmd.Flags().GetString("transport")
		if transport != "lan" {
			t.Fatalf("transport = %q", transport)
		}
		cmd.Println("first-run skip pair stub")
		return nil
	}
	locateNotifyBinary = func() (string, error) {
		t.Fatal("locateNotifyBinary should not run without hook targets")
		return "", nil
	}
	t.Cleanup(func() {
		webPairRun = oldWebPair
		locateNotifyBinary = oldLocate
	})

	out, _ := executeRootInput(t, "\n\n", "up", "--first-run", "--color", "never", "--no-logo")
	got := out.String()
	for _, want := range []string{"No detected agent config dirs", "Transport lan", "first-run skip pair stub"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected claude settings: %v", err)
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport.Mode != "lan" {
		t.Fatalf("saved transport = %q", cfg.Transport.Mode)
	}
}

func TestFirstRunIdempotentKeepsExistingTransportDefault(t *testing.T) {
	paths := withDefaultState(t)
	home := firstRunHome(t)
	cfg, meta, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Transport.Mode = "tailscale"
	if err := config.Save(meta.Path, cfg); err != nil {
		t.Fatal(err)
	}
	oldWebPair := webPairRun
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		transport, _ := cmd.Flags().GetString("transport")
		if transport != "tailscale" {
			t.Fatalf("transport = %q", transport)
		}
		cmd.Println("first-run idempotent pair stub")
		return nil
	}
	t.Cleanup(func() { webPairRun = oldWebPair })

	out, _ := executeRootInput(t, "\n\n", "up", "--first-run", "--color", "never", "--no-logo")
	got := out.String()
	for _, want := range []string{"No detected agent config dirs", "Select transport [2]", "Transport tailscale", "first-run idempotent pair stub"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected claude settings: %v", err)
	}
	cfg, _, err = config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport.Mode != "tailscale" {
		t.Fatalf("saved transport = %q", cfg.Transport.Mode)
	}
}

func TestFirstRunOffersOnlyV1AgentsAndWebTransports(t *testing.T) {
	withDefaultState(t)
	home := firstRunHome(t)
	for _, dir := range []string{
		".claude",
		".codex",
		".pi",
		".gemini",
		filepath.Join(".config", "opencode"),
	} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	db := openUpTestDB(t)
	cmd := Root()
	targets, err := firstRunDetectedHookTargets(cmd, db)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Kind != "agent" {
			t.Fatalf("unexpected first-run hook target: %+v", target)
		}
		got = append(got, target.Name)
	}
	if strings.Join(got, ",") != strings.Join(capability.V1Agents(), ",") {
		t.Fatalf("first-run agents = %v, want %v", got, capability.V1Agents())
	}
	transportModes := make([]string, 0, len(capability.V1WebTransports()))
	for _, choice := range pairTransportChoices("") {
		if !capability.IsV1WebTransport(choice.mode) {
			t.Fatalf("first-run offered non-v1 transport %q", choice.mode)
		}
		transportModes = append(transportModes, choice.mode)
	}
	if strings.Join(transportModes, ",") != strings.Join(capability.V1WebTransports(), ",") {
		t.Fatalf("first-run transports = %v, want %v", transportModes, capability.V1WebTransports())
	}
}

func firstRunHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func firstRunNotifyFixture(t *testing.T, home string) string {
	t.Helper()
	notify := filepath.Join(home, "bin", "onibi-notify")
	if err := os.MkdirAll(filepath.Dir(notify), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_NOTIFY_BIN", notify)
	return notify
}
