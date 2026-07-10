package matrix

import (
	"bytes"
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
	if got.UserID != state.UserID || got.DeviceID != state.DeviceID || got.AccountPickle != state.AccountPickle || !got.AccountShared {
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
