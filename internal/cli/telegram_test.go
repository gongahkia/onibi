package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTelegramSetupValidatesEnvironmentTokenBeforeOpeningState(t *testing.T) {
	t.Setenv("ONIBI_TELEGRAM_TOKEN", "invalid-test-token")
	root := Root()
	root.SetArgs([]string{"telegram", "setup"})
	if err := root.Execute(); err == nil || err.Error() != "token does not look like a BotFather token" {
		t.Fatalf("expected environment token validation, got %v", err)
	}
}

func TestTelegramStatusDisplaysCheckResult(t *testing.T) {
	for _, valid := range []bool{true, false} {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)
		report := map[string]any{"token": true, "enabled": true, "owner_paired": true, "token_valid": valid}
		want := "token_valid=true\nbot=@onibi_test_bot\n"
		if valid {
			report["bot"] = "@onibi_test_bot"
		} else {
			report["check_error"] = "Unauthorized"
			want = "token_valid=false\ncheck_error=Unauthorized\n"
		}
		if err := writeTelegramStatus(cmd, report); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing API check result: %s", out.String())
		}
	}
}

func TestCompletionRejectsUnknownShell(t *testing.T) {
	root := Root()
	root.SetArgs([]string{"completion", "not-a-shell"})
	if err := root.Execute(); err == nil {
		t.Fatal("unsupported shells must return an error")
	}
}
