package cli

import (
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/secrets"
)

func TestIRCSetupStatusDisableCLI(t *testing.T) {
	paths := withDefaultState(t)
	withDotenvSecretStore(t)
	out, _ := executeRoot(t, "irc", "setup", "--no-check", "--nick", "onibi_test_bot", "--account", "onibi_test_bot", "--password", "password", "--owner-nick", "owner", "--color", "never")
	if !strings.Contains(out.String(), "Owner token (shown once):") || !strings.Contains(out.String(), "DM syntax:") {
		t.Fatalf("setup output = %s", out.String())
	}
	out, _ = executeRoot(t, "irc", "status", "--json", "--color", "never")
	if !strings.Contains(out.String(), `"configured": true`) || strings.Contains(out.String(), "IRC_OWNER_TOKEN") {
		t.Fatalf("status output = %s", out.String())
	}
	executeRoot(t, "irc", "disable", "--color", "never")
	st, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Get(daemon.IRCSecretOwnerToken); err != nil || ok {
		t.Fatalf("owner token after disable = ok:%v err:%v", ok, err)
	}
}

func TestIRCSetupRequiresFlagsWithoutTerminal(t *testing.T) {
	withDotenvSecretStore(t)
	_, _, err := executeRootAllowError(t, "irc", "setup", "--no-check", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "--nick required") {
		t.Fatalf("err = %v", err)
	}
}
