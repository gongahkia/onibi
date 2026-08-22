package browser

import (
	"encoding/base64"
	"testing"
)

func TestContextOptionsAcceptsStorageStateOnlyInMemory(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"cookies":[],"origins":[]}`))
	options, err := contextOptions(encoded)
	if err != nil || options.StorageState == nil {
		t.Fatalf("contextOptions() = %#v, %v", options, err)
	}
	if _, err := contextOptions("not-base64"); err == nil {
		t.Fatal("invalid base64 storage state was accepted")
	}
	if _, err := contextOptions(base64.StdEncoding.EncodeToString([]byte(`not JSON`))); err == nil {
		t.Fatal("invalid storage-state JSON was accepted")
	}
}

func TestValidateRequestRequiresExplicitLoginSelectors(t *testing.T) {
	err := validateRequest(Request{URL: "https://booking.example.test/availability", LoginURL: "https://booking.example.test/login", Username: "reader", Password: "secret", PermittedHosts: []string{"booking.example.test"}})
	if err == nil {
		t.Fatal("incomplete login request was accepted")
	}
	if err := validateRequest(Request{URL: "https://booking.example.test/availability", PermittedHosts: []string{"booking.example.test"}}); err != nil {
		t.Fatalf("public read request = %v", err)
	}
}
