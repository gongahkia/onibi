package cli

import (
	"strings"
	"testing"
)

func TestRootLandingShowsPrimaryFlow(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "--color", "never")
	got := out.String()
	for _, want := range []string{"Onibi command center", "Start cockpit", "Pair phone", "Connect agents", "Telegram beta"} {
		if !strings.Contains(got, want) {
			t.Fatalf("landing missing %q:\n%s", want, got)
		}
	}
}

func TestLogoCommandRendersASCII(t *testing.T) {
	out, _ := executeRoot(t, "system", "logo", "--width", "24", "--color", "never")
	got := strings.TrimSpace(out.String())
	if got == "" {
		t.Fatal("empty logo")
	}
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 24 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}

func TestQuietRootSuppressesLogo(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "--quiet", "--color", "never")
	got := out.String()
	if strings.Contains(got, "#########") {
		t.Fatalf("quiet output included logo:\n%s", got)
	}
	if !strings.Contains(got, "Start cockpit") {
		t.Fatalf("quiet output missing flow:\n%s", got)
	}
}

func TestPairQuietHostPortOverride(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "phone", "pair", "--quiet", "--host", "phone.local", "--port", "9443", "--no-qr", "--color", "never")
	got := strings.TrimSpace(out.String())
	if !strings.HasPrefix(got, "https://phone.local:9443/pair/") {
		t.Fatalf("pair url = %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("quiet pair should print one line: %q", got)
	}
}
