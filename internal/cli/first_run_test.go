package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

	out, _ := executeRootInput(t, "all\n1\n2\n", "up", "--first-run", "--color", "never", "--no-logo")
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
