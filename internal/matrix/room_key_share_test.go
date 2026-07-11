package matrix

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestShareRoomKeyWithDevicesEncryptsAndSendsToDevice(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewOlmAccountState("@bob:example", "BOB", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	bobOTKs, err := OlmAccountOneTimeKeys(bob, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	var sent json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_matrix/client/v3/keys/query":
			var req struct {
				DeviceKeys map[string][]string `json:"device_keys"`
				Timeout    int                 `json:"timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.DeviceKeys[bob.UserID][0] != bob.DeviceID || req.Timeout != 2500 {
				t.Fatalf("query req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"device_keys": map[string]any{bob.UserID: map[string]any{bob.DeviceID: bob.DeviceKeys}}})
		case "/_matrix/client/v3/keys/claim":
			var req struct {
				OneTimeKeys map[string]map[string]string `json:"one_time_keys"`
				Timeout     int                          `json:"timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.OneTimeKeys[bob.UserID][bob.DeviceID] != KeyAlgorithmSignedCurve255 || req.Timeout != 2500 {
				t.Fatalf("claim req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"one_time_keys": map[string]any{bob.UserID: map[string]any{bob.DeviceID: map[string]any{"signed_curve25519:AAAA": map[string]any{"key": firstOneTimeKey(t, bobOTKs)}}}}})
		case "/_matrix/client/v3/sendToDevice/m.room.encrypted/txn-1":
			var req struct {
				Messages map[string]map[string]json.RawMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			sent = req.Messages[bob.UserID][bob.DeviceID]
			writeJSON(t, w, map[string]any{})
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	nextState, nextOutbound, err := c.ShareRoomKeyWithDevices(t.Context(), alice, outbound, roomKey, pickleKey, []RoomKeyShareTarget{{UserID: bob.UserID, DeviceID: bob.DeviceID}}, 2500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(nextState.OlmSessions) != 1 || len(nextOutbound.SharedWith[bob.UserID]) != 1 || nextOutbound.SharedWith[bob.UserID][0] != bob.DeviceID {
		t.Fatalf("state=%#v outbound=%#v", nextState.OlmSessions, nextOutbound.SharedWith)
	}
	var encrypted OlmEncryptedContent
	if err := json.Unmarshal(sent, &encrypted); err != nil {
		t.Fatal(err)
	}
	bob, _, plaintext, err := DecryptOlmFromDevice(bob, pickleKey, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Type          string         `json:"type"`
		Content       RoomKeyContent `json:"content"`
		Sender        string         `json:"sender"`
		Recipient     string         `json:"recipient"`
		Keys          map[string]string
		RecipientKeys map[string]string `json:"recipient_keys"`
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != EventRoomKey || payload.Sender != alice.UserID || payload.Recipient != bob.UserID || payload.Content.SessionID != roomKey.SessionID {
		t.Fatalf("payload=%#v", payload)
	}
	if payload.Keys["ed25519"] != alice.DeviceKeys.Keys["ed25519:ALICE"] || payload.RecipientKeys["ed25519"] != bob.DeviceKeys.Keys["ed25519:BOB"] {
		t.Fatalf("payload keys=%#v recipient=%#v", payload.Keys, payload.RecipientKeys)
	}
	if bob.OneTimeKeyCounts[KeyAlgorithmSignedCurve255] != 0 {
		t.Fatalf("bob one-time keys=%#v", bob.OneTimeKeyCounts)
	}
}

func TestShareRoomKeyWithDevicesRejectsMissingKeys(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/keys/query") {
			t.Fatalf("unexpected request = %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{"device_keys": map[string]any{}})
	}))
	defer srv.Close()
	_, _, err = New(srv.URL, "tok").ShareRoomKeyWithDevices(t.Context(), alice, outbound, roomKey, pickleKey, []RoomKeyShareTarget{{UserID: "@bob:example", DeviceID: "BOB"}}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "no device keys") {
		t.Fatalf("err = %v", err)
	}
}

func TestShareRoomKeyWithUsersResolvesAllDevices(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bobPhone, err := NewOlmAccountState("@bob:example", "PHONE", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	bobLaptop, err := NewOlmAccountState("@bob:example", "LAPTOP", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	phoneOTKs, err := OlmAccountOneTimeKeys(bobPhone, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	laptopOTKs, err := OlmAccountOneTimeKeys(bobLaptop, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_matrix/client/v3/keys/query":
			var req struct {
				DeviceKeys map[string][]string `json:"device_keys"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if devices, ok := req.DeviceKeys["@bob:example"]; !ok || len(devices) != 0 {
				t.Fatalf("query req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"device_keys": map[string]any{"@bob:example": map[string]any{
				"PHONE":  bobPhone.DeviceKeys,
				"LAPTOP": bobLaptop.DeviceKeys,
			}}})
		case "/_matrix/client/v3/keys/claim":
			var req struct {
				OneTimeKeys map[string]map[string]string `json:"one_time_keys"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.OneTimeKeys["@bob:example"]["PHONE"] != KeyAlgorithmSignedCurve255 || req.OneTimeKeys["@bob:example"]["LAPTOP"] != KeyAlgorithmSignedCurve255 {
				t.Fatalf("claim req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"one_time_keys": map[string]any{"@bob:example": map[string]any{
				"PHONE":  map[string]any{"signed_curve25519:PHONE": map[string]any{"key": firstOneTimeKey(t, phoneOTKs)}},
				"LAPTOP": map[string]any{"signed_curve25519:LAPTOP": map[string]any{"key": firstOneTimeKey(t, laptopOTKs)}},
			}}})
		case "/_matrix/client/v3/sendToDevice/m.room.encrypted/txn-1":
			var req struct {
				Messages map[string]map[string]json.RawMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			sent = req.Messages
			writeJSON(t, w, map[string]any{})
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	nextState, nextOutbound, err := c.ShareRoomKeyWithUsers(t.Context(), alice, outbound, roomKey, pickleKey, []string{"@bob:example"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent["@bob:example"]) != 2 || sent["@bob:example"]["PHONE"] == nil || sent["@bob:example"]["LAPTOP"] == nil {
		t.Fatalf("sent = %#v", sent)
	}
	if len(nextState.OlmSessions) != 2 || len(nextOutbound.SharedWith["@bob:example"]) != 2 {
		t.Fatalf("state=%#v outbound=%#v", nextState.OlmSessions, nextOutbound.SharedWith)
	}
}

func TestShareRoomKeyWithTrustedUsersSkipsUntrustedDevices(t *testing.T) {
	pickleKey := []byte("pickle-key")
	alice, err := NewOlmAccountState("@alice:example", "ALICE", pickleKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	bobPhone, err := NewOlmAccountState("@bob:example", "PHONE", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	bobLaptop, err := NewOlmAccountState("@bob:example", "LAPTOP", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	phoneOTKs, err := OlmAccountOneTimeKeys(bobPhone, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	alice.TrustedDevices = map[string][]string{"@bob:example": {"PHONE"}}
	var sent map[string]map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_matrix/client/v3/keys/query":
			writeJSON(t, w, map[string]any{"device_keys": map[string]any{"@bob:example": map[string]any{
				"PHONE":  bobPhone.DeviceKeys,
				"LAPTOP": bobLaptop.DeviceKeys,
			}}})
		case "/_matrix/client/v3/keys/claim":
			var req struct {
				OneTimeKeys map[string]map[string]string `json:"one_time_keys"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if len(req.OneTimeKeys["@bob:example"]) != 1 || req.OneTimeKeys["@bob:example"]["PHONE"] != KeyAlgorithmSignedCurve255 {
				t.Fatalf("claim req = %#v", req)
			}
			writeJSON(t, w, map[string]any{"one_time_keys": map[string]any{"@bob:example": map[string]any{
				"PHONE": map[string]any{"signed_curve25519:PHONE": map[string]any{"key": firstOneTimeKey(t, phoneOTKs)}},
			}}})
		case "/_matrix/client/v3/sendToDevice/m.room.encrypted/txn-1":
			var req struct {
				Messages map[string]map[string]json.RawMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			sent = req.Messages
			writeJSON(t, w, map[string]any{})
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	nextState, nextOutbound, err := c.ShareRoomKeyWithTrustedUsers(t.Context(), alice, outbound, roomKey, pickleKey, []string{"@bob:example"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent["@bob:example"]) != 1 || sent["@bob:example"]["PHONE"] == nil || sent["@bob:example"]["LAPTOP"] != nil {
		t.Fatalf("sent = %#v", sent)
	}
	if len(nextState.OlmSessions) != 1 || len(nextOutbound.SharedWith["@bob:example"]) != 1 || nextOutbound.SharedWith["@bob:example"][0] != "PHONE" {
		t.Fatalf("state=%#v outbound=%#v", nextState.OlmSessions, nextOutbound.SharedWith)
	}
}
