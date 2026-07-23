package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/secrets"
	webtransport "github.com/gongahkia/onibi/internal/web/transport"
)

func TestNgrokSetupStatusDisableCLI(t *testing.T) {
	paths := withDefaultState(t)
	withDotenvSecretStore(t)
	t.Setenv(webtransport.NgrokDomainEnv, "demo.ngrok-free.app")

	out, _ := executeRoot(t, "transport", "ngrok", "setup", "--authtoken", "ngrok-token-1234567890", "--color", "never")
	if !strings.Contains(out.String(), "Stored ngrok authtoken") {
		t.Fatalf("setup output:\n%s", out.String())
	}
	out, _ = executeRoot(t, "transport", "ngrok", "status", "--json", "--color", "never")
	var status ngrokStatusReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("status json: %v\n%s", err, out.String())
	}
	if !status.Authtoken || !status.Domain {
		t.Fatalf("status = %+v", status)
	}
	executeRoot(t, "transport", "ngrok", "disable", "--color", "never")
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Get(webtransport.NgrokSecretAuthtoken); err != nil || ok {
		t.Fatalf("token after disable ok=%v err=%v", ok, err)
	}
}
