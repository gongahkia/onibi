package matrix

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

func TestCryptoStateRoundTripEncrypted(t *testing.T) {
	db := matrixStateDB(t)
	state := CryptoState{
		UserID:        "@bot:example",
		DeviceID:      "ONIBI",
		PickleKey:     base64.RawStdEncoding.EncodeToString([]byte("secret-pickle-key")),
		AccountPickle: "secret-olm-account-pickle",
		AccountShared: true,
		DeviceKeys: &DeviceKeys{
			UserID:     "@bot:example",
			DeviceID:   "ONIBI",
			Algorithms: []string{AlgorithmOlmV1, AlgorithmMegolmV1},
			Keys:       map[string]string{"curve25519:ONIBI": "curve-secret", "ed25519:ONIBI": "ed-secret"},
		},
		OneTimeKeyCounts: map[string]int{KeyAlgorithmSignedCurve255: 7},
		NextBatch:        "sync-token",
		OlmSessions: map[string]OlmSessionState{
			"@alice:example/ALICE": {
				UserID:    "@alice:example",
				DeviceID:  "ALICE",
				SenderKey: "alice-curve-secret",
				SessionID: "olm-session",
				Pickle:    "secret-olm-session-pickle",
			},
		},
		MegolmOutboundSessions: map[string]MegolmOutboundState{
			"!room:example": {
				RoomID:    "!room:example",
				SessionID: "outbound-megolm-session",
				Pickle:    "secret-outbound-megolm-pickle",
				SharedWith: map[string][]string{
					"@alice:example": {"ALICE"},
				},
			},
		},
		MegolmInboundSessions: map[string]MegolmInboundState{
			"!room:example/alice-curve-secret/inbound-megolm-session": {
				RoomID:    "!room:example",
				SenderKey: "alice-curve-secret",
				SessionID: "inbound-megolm-session",
				Pickle:    "secret-inbound-megolm-pickle",
			},
		},
		SASTransactions: map[string]SASTransactionState{
			"txn-1": {
				TransactionID:      "txn-1",
				UserID:             "@alice:example",
				DeviceID:           "ALICE",
				State:              "started",
				EphemeralPublicKey: "sas-ephemeral-secret",
				Commitment:         "sas-commitment-secret",
			},
		},
		TrustedDevices: map[string][]string{"@alice:example": {"ALICE"}},
	}
	if err := SaveCryptoState(t.Context(), db, state); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := db.KVGet(t.Context(), CryptoStateKVKey)
	if err != nil || !ok {
		t.Fatalf("raw state ok=%v err=%v", ok, err)
	}
	for _, secret := range []string{
		"secret-olm-account-pickle",
		state.PickleKey,
		"curve-secret",
		"sync-token",
		"secret-olm-session-pickle",
		"secret-outbound-megolm-pickle",
		"secret-inbound-megolm-pickle",
		"sas-ephemeral-secret",
		"sas-commitment-secret",
	} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("raw state contains %q", secret)
		}
	}
	got, ok, err := LoadCryptoState(t.Context(), db)
	if err != nil || !ok {
		t.Fatalf("load ok=%v err=%v", ok, err)
	}
	if got.UserID != state.UserID || got.DeviceID != state.DeviceID || got.PickleKey != state.PickleKey || got.AccountPickle != state.AccountPickle || !got.AccountShared {
		t.Fatalf("state = %#v", got)
	}
	if got.DeviceKeys.Keys["curve25519:ONIBI"] != "curve-secret" {
		t.Fatalf("device keys = %#v", got.DeviceKeys)
	}
	if got.OneTimeKeyCounts[KeyAlgorithmSignedCurve255] != 7 || got.NextBatch != "sync-token" {
		t.Fatalf("state = %#v", got)
	}
	if got.OlmSessions["@alice:example/ALICE"].Pickle != "secret-olm-session-pickle" {
		t.Fatalf("olm sessions = %#v", got.OlmSessions)
	}
	if got.MegolmOutboundSessions["!room:example"].Pickle != "secret-outbound-megolm-pickle" {
		t.Fatalf("outbound sessions = %#v", got.MegolmOutboundSessions)
	}
	if got.MegolmInboundSessions["!room:example/alice-curve-secret/inbound-megolm-session"].Pickle != "secret-inbound-megolm-pickle" {
		t.Fatalf("inbound sessions = %#v", got.MegolmInboundSessions)
	}
	if got.SASTransactions["txn-1"].Commitment != "sas-commitment-secret" {
		t.Fatalf("sas transactions = %#v", got.SASTransactions)
	}
	if got.TrustedDevices["@alice:example"][0] != "ALICE" {
		t.Fatalf("trusted devices = %#v", got.TrustedDevices)
	}
}

func TestCryptoStateMissingAndValidation(t *testing.T) {
	db := matrixStateDB(t)
	if _, ok, err := LoadCryptoState(t.Context(), db); err != nil || ok {
		t.Fatalf("missing ok=%v err=%v", ok, err)
	}
	if err := SaveCryptoState(t.Context(), nil, CryptoState{}); err == nil {
		t.Fatal("expected nil db error")
	}
	if err := SaveCryptoState(t.Context(), db, CryptoState{UserID: "@bot:example"}); err == nil {
		t.Fatal("expected missing device id error")
	}
}

func TestEnsureCryptoStateCreatesEncryptedOlmAccount(t *testing.T) {
	db := matrixStateDB(t)
	state, pickleKey, created, err := EnsureCryptoState(t.Context(), db, "@bot:example", "ONIBI", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(pickleKey) != 32 || state.PickleKey == "" || state.AccountPickle == "" || state.DeviceKeys == nil {
		t.Fatalf("created=%v key=%d state=%#v", created, len(pickleKey), state)
	}
	otks, err := OlmAccountOneTimeKeys(state, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(otks) != 2 {
		t.Fatalf("one-time keys = %#v", otks)
	}
	raw, ok, err := db.KVGet(t.Context(), CryptoStateKVKey)
	if err != nil || !ok {
		t.Fatalf("raw state ok=%v err=%v", ok, err)
	}
	for _, secret := range []string{state.PickleKey, state.AccountPickle, state.DeviceKeys.Keys["curve25519:ONIBI"]} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("raw state contains %q", secret)
		}
	}
	loaded, loadedKey, created, err := EnsureCryptoState(t.Context(), db, "@bot:example", "ONIBI", 9)
	if err != nil {
		t.Fatal(err)
	}
	if created || !bytes.Equal(loadedKey, pickleKey) || loaded.AccountPickle != state.AccountPickle || loaded.PickleKey != state.PickleKey {
		t.Fatalf("created=%v loaded=%#v", created, loaded)
	}
	if _, _, _, err := EnsureCryptoState(t.Context(), db, "@other:example", "ONIBI", 2); err == nil {
		t.Fatal("expected user mismatch")
	}
	if _, _, _, err := EnsureCryptoState(t.Context(), db, "@bot:example", "OTHER", 2); err == nil {
		t.Fatal("expected device mismatch")
	}
}

func TestEnsureCryptoStateInitializesLegacyEmptyState(t *testing.T) {
	db := matrixStateDB(t)
	if err := SaveCryptoState(t.Context(), db, CryptoState{UserID: "@bot:example", DeviceID: "ONIBI"}); err != nil {
		t.Fatal(err)
	}
	state, pickleKey, created, err := EnsureCryptoState(t.Context(), db, "@bot:example", "ONIBI", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(pickleKey) != 32 || state.PickleKey == "" || state.AccountPickle == "" || state.DeviceKeys == nil {
		t.Fatalf("created=%v key=%d state=%#v", created, len(pickleKey), state)
	}
}

func TestE2EEBootstrap(t *testing.T) {
	db := matrixStateDB(t)
	state, pickleKey, created, err := EnsureCryptoState(t.Context(), db, "@bot:example", "ONIBI", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(pickleKey) != 32 || state.DeviceKeys == nil {
		t.Fatalf("created=%v pickle=%d state=%#v", created, len(pickleKey), state)
	}
	state.SASTransactions = map[string]SASTransactionState{
		"txn-1": {TransactionID: "txn-1", UserID: "@owner:example", DeviceID: "OWNER", State: SASStateDone},
	}
	state, err = MarkDeviceTrustedFromSAS(state, "txn-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCryptoState(t.Context(), db, state); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadCryptoState(t.Context(), db)
	if err != nil || !ok {
		t.Fatalf("load ok=%v err=%v", ok, err)
	}
	if !IsDeviceTrusted(loaded, "@owner:example", "OWNER") {
		t.Fatalf("trusted devices = %#v", loaded.TrustedDevices)
	}
	targets, err := TrustedRoomKeyShareTargets(loaded, []RoomKeyShareTarget{{UserID: "@owner:example", DeviceID: "OWNER"}})
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets=%#v err=%v", targets, err)
	}
}

func matrixStateDB(t *testing.T) *store.DB {
	t.Helper()
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "matrix-state.sqlite"), store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
