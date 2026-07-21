package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

func TestIRCSetupStatusDisableCLI(t *testing.T) {
	paths := withDefaultState(t)
	enableExperimentalProviders(t, paths)
	withDotenvSecretStore(t)

	out, _ := executeRoot(t, "experimental", "irc", "setup", "--nick", "onibi_test", "--username", "onibi_test", "--password", "password", "--owner-nick", "owner", "--owner-account", "owner-account", "--color", "never")
	if out.String() == "" {
		t.Fatal("setup output empty")
	}
	out, _ = executeRoot(t, "experimental", "irc", "status", "--json", "--color", "never")
	var status ircStatusReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("status json: %v\n%s", err, out.String())
	}
	if !status.Configured || !status.Password || status.Address == "" || status.Nick != "onibi_test" || status.Username != "onibi_test" || status.OwnerNick != "owner" || status.OwnerAccount != "owner-account" {
		t.Fatalf("status = %+v", status)
	}
	executeRoot(t, "experimental", "irc", "disable", "--color", "never")

	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Get(daemon.IRCSecretSASLPassword); err != nil || ok {
		t.Fatalf("password after disable ok=%v err=%v", ok, err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, key := range []string{daemon.IRCKVAddress, daemon.IRCKVNick, daemon.IRCKVUsername, daemon.IRCKVOwnerNick, daemon.IRCKVOwnerAccount} {
		if _, ok, err := db.KVGetString(context.Background(), key); err != nil || ok {
			t.Fatalf("%s after disable ok=%v err=%v", key, ok, err)
		}
	}
}

func TestIRCSetupRejectsMissingOwnerAccount(t *testing.T) {
	paths := withDefaultState(t)
	enableExperimentalProviders(t, paths)
	withDotenvSecretStore(t)
	_, _, err := executeRootAllowError(t, "experimental", "irc", "setup", "--nick", "onibi_test", "--username", "onibi_test", "--password", "password", "--owner-nick", "owner", "--color", "never")
	if err == nil {
		t.Fatal("setup without owner account succeeded")
	}
}
