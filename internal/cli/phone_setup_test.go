package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/spf13/cobra"
)

func TestWritePhoneSetupGuideForLocalTrust(t *testing.T) {
	var out bytes.Buffer
	writePhoneSetupGuide(&out, phoneSetupState{
		Transport:    "lan",
		MobileConfig: "/state/onibi-local-ca.mobileconfig",
		CACert:       "/state/onibi-local-ca.crt",
		CertReady:    true,
	})
	for _, want := range []string{"iPhone/iPad:", "Android:", "do not download a CA file from the local network", "/state/onibi-local-ca.mobileconfig", "/state/onibi-local-ca.crt", "private CA key stays on this Mac"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("guide missing %q: %q", want, out.String())
		}
	}
	if strings.Contains(strings.ToLower(out.String()), "airdrop") {
		t.Fatalf("guide mentions AirDrop: %q", out.String())
	}
}

func TestWritePhoneSetupGuideBeforeCertGeneration(t *testing.T) {
	var out bytes.Buffer
	writePhoneSetupGuide(&out, phoneSetupState{Transport: "lan"})
	if !strings.Contains(out.String(), "onibi up --transport=lan") {
		t.Fatalf("guide = %q", out.String())
	}
}

func TestNeedsLocalPhoneTrust(t *testing.T) {
	for _, mode := range []string{"lan", "tailscale-private", "wireguard", "zerotier", "auto"} {
		if !needsLocalPhoneTrust(mode) {
			t.Fatalf("%s should require local phone trust", mode)
		}
	}
	for _, mode := range []string{"cloudflare-quick", "ngrok", "telegram", "irc"} {
		if needsLocalPhoneTrust(mode) {
			t.Fatalf("%s should not require local phone trust", mode)
		}
	}
}

func TestPrintDoctorPhoneSetupBeforeCertGeneration(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		StateDir: filepath.Join(dir, "state"),
		DBFile:   filepath.Join(dir, "state", "onibi.sqlite"),
		EnvFile:  filepath.Join(dir, "state", ".env"),
		LogDir:   filepath.Join(dir, "state", "logs"),
		Socket:   filepath.Join(dir, "state", "onibi.sock"),
		Config:   filepath.Join(dir, "state", "config.yaml"),
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	printDoctorPhoneSetup(cmd, paths, "lan")
	if !strings.Contains(out.String(), "onibi up --transport=lan") {
		t.Fatalf("doctor guide = %q", out.String())
	}
}
