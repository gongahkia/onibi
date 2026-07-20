package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPromptPairTransportSelectsTailscalePrivate(t *testing.T) {
	cmd, out := transportPromptCmd("2\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "tailscale-private" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
	for _, want := range []string{"Web cockpit transport", "Tailscale Serve", "Cloudflare Quick", "COVERAGE"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("prompt output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPromptPairTransportKeepsCurrentOnEnter(t *testing.T) {
	cmd, _ := transportPromptCmd("\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "auto" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
}

func TestPromptPairTransportExcludesProviderControl(t *testing.T) {
	cmd, out := transportPromptCmd("telegram\n1\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "lan" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
	for _, unwanted := range []string{"Chat", "Notify-only", "Telegram", "Signal", "Pushover"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("prompt exposes deferred provider %q:\n%s", unwanted, out.String())
		}
	}
}

func TestPromptPairTransportSelectsCloudflareQuick(t *testing.T) {
	cmd, _ := transportPromptCmd("cloudflare-quick\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "cloudflare-quick" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
}

func TestPromptPairTransportCancel(t *testing.T) {
	cmd, _ := transportPromptCmd("q\n")
	withPromptTTY(t, true)
	_, prompted, err := promptPairTransport(cmd, "lan")
	if !prompted || err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("prompted=%v err=%v", prompted, err)
	}
}

func TestPromptPairTransportSkipsNonInteractive(t *testing.T) {
	cmd, _ := transportPromptCmd("2\n")
	withPromptTTY(t, false)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if prompted || got != "lan" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
}

func transportPromptCmd(input string) (*cobra.Command, *bytes.Buffer) {
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	return cmd, out
}

func withPromptTTY(t *testing.T, yes bool) {
	t.Helper()
	oldIn := inputIsTerminal
	oldOut := outputIsTerminal
	inputIsTerminal = func(any) bool { return yes }
	outputIsTerminal = func(any) bool { return yes }
	t.Cleanup(func() {
		inputIsTerminal = oldIn
		outputIsTerminal = oldOut
	})
}
