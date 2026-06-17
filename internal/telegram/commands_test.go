package telegram

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterCommands(t *testing.T) {
	mock := NewMock(nil)
	if err := RegisterCommands(context.Background(), mock); err != nil {
		t.Fatal(err)
	}
	got := mock.RegisteredCommands()
	if len(got) != 1 {
		t.Fatalf("calls = %d", len(got))
	}
	if len(got[0].Commands) == 0 || got[0].Commands[0].Command != "status" {
		t.Fatalf("commands = %#v", got[0].Commands)
	}
	foundSecure := false
	foundPing := false
	for _, c := range got[0].Commands {
		if c.Command == "secure" {
			foundSecure = true
		}
		if c.Command == "ping" {
			foundPing = true
		}
	}
	if !foundSecure {
		t.Fatalf("commands missing secure = %#v", got[0].Commands)
	}
	if !foundPing {
		t.Fatalf("commands missing ping = %#v", got[0].Commands)
	}
}

func TestHelpTextGrouped(t *testing.T) {
	got := HelpText()
	for _, want := range []string{"Sessions:", "Controls:", "Prompts:", "Security:", "/secure - open encrypted controls", "Use /help <command> for details."} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q:\n%s", want, got)
		}
	}
}

func TestHelpDetail(t *testing.T) {
	got := HelpDetail("prompt")
	if !strings.Contains(got, "/prompt <text>") || !strings.Contains(got, "Examples:") {
		t.Fatalf("detail = %q", got)
	}
}

func TestHelpDetailPing(t *testing.T) {
	got := HelpDetail("ping")
	if !strings.Contains(got, "/ping") || !strings.Contains(got, "daemon uptime") {
		t.Fatalf("detail = %q", got)
	}
}
