package matrix

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultSASStartAndAcceptShapes(t *testing.T) {
	start := DefaultSASStart("txn-1", "ONIBI")
	if start.Method != VerificationMethodSASV1 || start.TransactionID != "txn-1" || start.FromDevice != "ONIBI" {
		t.Fatalf("start = %#v", start)
	}
	if !containsString(start.KeyAgreementProtocols, KeyAgreementCurve25519SHA256) || !containsString(start.Hashes, HashSHA256) || !containsString(start.ShortAuthenticationString, SASEmoji) {
		t.Fatalf("start algorithms = %#v", start)
	}
	accept := DefaultSASAccept("txn-1", "commit")
	if accept.Method != VerificationMethodSASV1 || accept.Commitment != "commit" || accept.MessageAuthenticationCode != MACHKDFHMACSHA256V2 {
		t.Fatalf("accept = %#v", accept)
	}
}

func TestSASCommitmentUsesCanonicalStartContent(t *testing.T) {
	start := DefaultSASStart("txn-1", "ONIBI")
	got, err := SASCommitment("curve-public", start)
	if err != nil {
		t.Fatal(err)
	}
	canonical := `{"from_device":"ONIBI","hashes":["sha256"],"key_agreement_protocols":["curve25519-hkdf-sha256","curve25519"],"message_authentication_codes":["hkdf-hmac-sha256.v2","hkdf-hmac-sha256"],"method":"m.sas.v1","short_authentication_string":["decimal","emoji"],"transaction_id":"txn-1"}`
	sum := sha256.Sum256([]byte("curve-public" + canonical))
	want := base64.RawStdEncoding.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("commitment = %q want %q", got, want)
	}
	if _, err := SASCommitment("", start); err == nil {
		t.Fatal("expected empty ephemeral key error")
	}
}

func TestSendVerificationToDeviceShape(t *testing.T) {
	var req ToDeviceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/sendToDevice/m.key.verification.start/txn-1") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, w, map[string]any{})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	err := c.SendVerificationToDevice(t.Context(), EventKeyVerificationStart, "@alice:example", "ALICE", DefaultSASStart("txn-1", "ONIBI"))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := req.Messages["@alice:example"]["ALICE"].(map[string]any)
	if !ok || raw["method"] != VerificationMethodSASV1 || raw["from_device"] != "ONIBI" {
		t.Fatalf("messages = %#v", req.Messages)
	}
}

func TestVerificationToDeviceValidation(t *testing.T) {
	if _, err := VerificationToDeviceMessages("", "ALICE", DefaultSASStart("txn", "ONIBI")); err == nil {
		t.Fatal("expected user validation error")
	}
	if _, err := VerificationToDeviceMessages("@alice:example", "", DefaultSASStart("txn", "ONIBI")); err == nil {
		t.Fatal("expected device validation error")
	}
	if _, err := VerificationToDeviceMessages("@alice:example", "ALICE", nil); err == nil {
		t.Fatal("expected content validation error")
	}
}
