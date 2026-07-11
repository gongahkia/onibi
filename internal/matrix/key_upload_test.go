package matrix

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"maunium.net/go/mautrix/crypto/signatures"
	"maunium.net/go/mautrix/id"
)

func TestSignedDeviceAndOneTimeKeysVerify(t *testing.T) {
	pickleKey := []byte("pickle-key")
	state, err := NewOlmAccountState("@bot:example", "ONIBI", pickleKey, 2)
	if err != nil {
		t.Fatal(err)
	}
	deviceKeys, err := SignedDeviceKeys(state, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	ed25519 := id.Ed25519(state.DeviceKeys.Keys["ed25519:ONIBI"])
	if ok, err := signatures.VerifySignatureJSON(deviceKeys, id.UserID(state.UserID), state.DeviceID, ed25519); err != nil || !ok {
		t.Fatalf("device signature ok=%v err=%v", ok, err)
	}
	otks, err := SignedOneTimeKeys(state, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(otks) != 2 {
		t.Fatalf("one-time keys = %#v", otks)
	}
	for _, raw := range otks {
		key, err := decodeSignedOneTimeKeyTest(raw)
		if err != nil {
			t.Fatal(err)
		}
		if key.Key == "" || key.Signatures[state.UserID]["ed25519:ONIBI"] == "" {
			t.Fatalf("signed key = %#v", key)
		}
		if ok, err := signatures.VerifySignatureJSON(key, id.UserID(state.UserID), state.DeviceID, ed25519); err != nil || !ok {
			t.Fatalf("one-time signature ok=%v err=%v", ok, err)
		}
	}
}

func decodeSignedOneTimeKeyTest(raw any) (SignedOneTimeKey, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return SignedOneTimeKey{}, err
	}
	var out SignedOneTimeKey
	if err := json.Unmarshal(b, &out); err != nil {
		return SignedOneTimeKey{}, err
	}
	return out, nil
}

func TestUploadCryptoKeysSignsAndMarksPublished(t *testing.T) {
	pickleKey := []byte("pickle-key")
	state, err := NewOlmAccountState("@bot:example", "ONIBI", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	var req KeysUploadRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_matrix/client/v3/keys/upload" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, w, map[string]any{"one_time_key_counts": map[string]int{KeyAlgorithmSignedCurve255: 1}})
	}))
	defer srv.Close()
	next, resp, err := New(srv.URL, "tok").UploadCryptoKeys(t.Context(), state, pickleKey, true)
	if err != nil {
		t.Fatal(err)
	}
	if req.DeviceKeys == nil || req.DeviceKeys.Signatures[state.UserID]["ed25519:ONIBI"] == "" || len(req.OneTimeKeys) != 1 {
		t.Fatalf("request = %#v", req)
	}
	if !next.AccountShared || next.AccountPickle == state.AccountPickle || resp.OneTimeKeyCounts[KeyAlgorithmSignedCurve255] != 1 {
		t.Fatalf("next=%#v resp=%#v", next, resp)
	}
	remaining, err := OlmAccountOneTimeKeys(next, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining one-time keys = %#v", remaining)
	}
}
