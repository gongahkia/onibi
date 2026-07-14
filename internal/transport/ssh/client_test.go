package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestHostKeyCallbackAcceptsUnknownHostAndPersistsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	key := testPublicKey(t)
	called := false
	cb := hostKeyCallback(path, func(host string, remote net.Addr, got xssh.PublicKey) (bool, error) {
		called = true
		if host != "example.com:2222" {
			t.Fatalf("host = %q", host)
		}
		if got.Type() != key.Type() {
			t.Fatalf("key type = %q", got.Type())
		}
		return true, nil
	})
	if err := cb("example.com:2222", &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}, key); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("prompt was not called")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[example.com]:2222 "+key.Type()) {
		t.Fatalf("known_hosts = %q", data)
	}
	if err := cb("example.com:2222", &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}, key); err != nil {
		t.Fatal(err)
	}
}

func TestHostKeyCallbackRejectsMismatchedKnownHostWithoutPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	oldKey := testPublicKey(t)
	newKey := testPublicKey(t)
	if err := appendKnownHost(path, "example.com:22", oldKey); err != nil {
		t.Fatal(err)
	}
	cb := hostKeyCallback(path, func(string, net.Addr, xssh.PublicKey) (bool, error) {
		t.Fatal("prompt called for mismatched host key")
		return false, nil
	})
	err := cb("example.com:22", &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 22}, newKey)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "refusing automatic replacement") || !strings.Contains(err.Error(), xssh.FingerprintSHA256(newKey)) {
		t.Fatalf("mismatch diagnostic = %v", err)
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) || len(keyErr.Want) == 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultPromptRequiresYes(t *testing.T) {
	var out bytes.Buffer
	key := testPublicKey(t)
	ok, err := defaultPrompt(strings.NewReader("no\n"), &out)("example.com:22", nil, key)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("accepted no")
	}
	ok, err = defaultPrompt(strings.NewReader("yes\n"), &out)("example.com:22", nil, key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("rejected yes")
	}
	if !strings.Contains(out.String(), xssh.FingerprintSHA256(key)) {
		t.Fatalf("prompt missing fingerprint: %q", out.String())
	}
}

func TestNormalizeDialAddressDefaultsPort(t *testing.T) {
	if got := normalizeDialAddress("example.com"); got != "example.com:22" {
		t.Fatalf("address = %q", got)
	}
	if got := normalizeDialAddress("example.com:2200"); got != "example.com:2200" {
		t.Fatalf("address = %q", got)
	}
}

func testPublicKey(t *testing.T) xssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
