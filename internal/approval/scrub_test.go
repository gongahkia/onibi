package approval

import (
	"strings"
	"testing"
)

func TestScrubPEM(t *testing.T) {
	in := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA1234\n-----END RSA PRIVATE KEY-----\nafter"
	out := Scrub(in)
	if strings.Contains(out, "MIIEpAIB") {
		t.Fatalf("PEM body leaked: %s", out)
	}
	if !strings.Contains(out, placeholder) {
		t.Fatalf("missing placeholder: %s", out)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatalf("non-secret context lost: %s", out)
	}
}

func TestScrubAWS(t *testing.T) {
	out := Scrub("AKIAIOSFODNN7EXAMPLE")
	if out != placeholder {
		t.Fatalf("expected placeholder, got %q", out)
	}
}

func TestScrubGitHubTokens(t *testing.T) {
	cases := []string{
		"ghp_" + strings.Repeat("a", 36),
		"gho_" + strings.Repeat("b", 40),
		"ghs_" + strings.Repeat("c", 40),
	}
	for _, c := range cases {
		out := Scrub(c)
		if strings.Contains(out, "_a") || strings.Contains(out, "_b") || strings.Contains(out, "_c") {
			t.Fatalf("token leaked: %q -> %q", c, out)
		}
	}
}

func TestScrubBearer(t *testing.T) {
	out := Scrub(`Authorization: Bearer abc123def456ghi789jkl000`)
	if strings.Contains(out, "abc123def456") {
		t.Fatalf("bearer leaked: %s", out)
	}
}

func TestScrubAssignments(t *testing.T) {
	out := Scrub(`PASSWORD="super-secret-value"`)
	if strings.Contains(out, "super-secret") {
		t.Fatalf("password leaked: %s", out)
	}
	if !strings.Contains(out, `PASSWORD="`+placeholder+`"`) {
		t.Fatalf("expected redacted form, got %s", out)
	}
}

func TestScrubJSONFields(t *testing.T) {
	out := Scrub(`{"api_key": "abc123xyz", "user": "alice"}`)
	if strings.Contains(out, "abc123xyz") {
		t.Fatalf("api_key leaked: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("non-secret field clobbered: %s", out)
	}
}

func TestScrubCLISecretFlags(t *testing.T) {
	out := Scrub(`deploy --token raw-sensitive-value --api-key=another-secret`)
	if strings.Contains(out, "raw-sensitive-value") || strings.Contains(out, "another-secret") {
		t.Fatalf("CLI secret leaked: %s", out)
	}
	if !strings.Contains(out, "--token "+placeholder) || !strings.Contains(out, "--api-key="+placeholder) {
		t.Fatalf("CLI flag context missing: %s", out)
	}
}

func TestScrubLeavesNonSecrets(t *testing.T) {
	plain := `rm -rf /tmp/data && echo done`
	if Scrub(plain) != plain {
		t.Fatalf("plain text modified: %s -> %s", plain, Scrub(plain))
	}
}

func TestScrubStripeKey(t *testing.T) {
	out := Scrub("sk_live_" + strings.Repeat("9", 24))
	if strings.Contains(out, "999") {
		t.Fatalf("stripe key leaked: %s", out)
	}
}
