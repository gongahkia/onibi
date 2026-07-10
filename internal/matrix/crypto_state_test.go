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
	}
	if err := SaveCryptoState(t.Context(), db, state); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := db.KVGet(t.Context(), CryptoStateKVKey)
	if err != nil || !ok {
		t.Fatalf("raw state ok=%v err=%v", ok, err)
	}
	for _, secret := range []string{"secret-olm-account-pickle", "curve-secret", "sync-token"} {
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
