package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPromptPairTransportSelectsTailscale(t *testing.T) {
	cmd, out := transportPromptCmd("1\n2\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted {
		t.Fatal("prompt not shown")
	}
	if got != "tailscale" {
		t.Fatalf("transport = %q", got)
	}
	if !strings.Contains(out.String(), "Connection category") || !strings.Contains(out.String(), "Tailscale Funnel") || !strings.Contains(out.String(), "Cloudflare Tunnel") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestPromptPairTransportEnterKeepsCurrent(t *testing.T) {
	cmd, _ := transportPromptCmd("\n\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "auto" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
}

func TestPromptPairTransportSelectsTelegram(t *testing.T) {
	cmd, out := transportPromptCmd("2\n1\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "telegram" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
	if !strings.Contains(out.String(), "Telegram") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestPromptPairTransportDefaultsToTelegramProvider(t *testing.T) {
	cmd, _ := transportPromptCmd("\n\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "telegram" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
}

func TestPromptPairTransportRepromptsForUnavailableNotifyOnly(t *testing.T) {
	cmd, out := transportPromptCmd("3\n\n1\n1\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "lan" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
	if !strings.Contains(out.String(), "Pushover") || !strings.Contains(out.String(), "No notify-only providers are enabled yet") {
		t.Fatalf("missing unavailable notice: %q", out.String())
	}
}

func TestPromptPairTransportRepromptsForUnavailableWebProvider(t *testing.T) {
	cmd, out := transportPromptCmd("1\ncloudflare\n1\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "lan" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
	if !strings.Contains(out.String(), "not enabled in this build") {
		t.Fatalf("missing unavailable notice: %q", out.String())
	}
}

func TestPromptPairTransportBackFromProvider(t *testing.T) {
	cmd, out := transportPromptCmd("2\nb\n1\n3\n")
	withPromptTTY(t, true)
	got, prompted, err := promptPairTransport(cmd, "lan")
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || got != "auto" {
		t.Fatalf("prompted=%v transport=%q", prompted, got)
	}
	if strings.Count(out.String(), "Connection category") != 2 {
		t.Fatalf("prompt output = %q", out.String())
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
