package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/spf13/cobra"
)

func TestPrintKeychainPreflight(t *testing.T) {
	cmd := &cobra.Command{}
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	printKeychainPreflight(cmd, secrets.BackendKeychain)

	for _, want := range []string{"macOS may request Keychain access", "Onibi — onibi.store.key.v1", "choose Allow", "never enter it in this terminal"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr missing %q: %q", want, errOut.String())
		}
	}
}

func TestPrintKeychainPreflightSkipsOtherBackends(t *testing.T) {
	cmd := &cobra.Command{}
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	printKeychainPreflight(cmd, secrets.BackendDotenv)

	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q", errOut.String())
	}
}
