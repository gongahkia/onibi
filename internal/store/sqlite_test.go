package store

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestKVRoundtrip(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := db.KVSetString(ctx, "owner_id", "42"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := db.KVGetString(ctx, "owner_id")
	if err != nil || !ok || v != "42" {
		t.Fatalf("got %q, %v, %v", v, ok, err)
	}
}

func TestKVExpire(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).Unix()
	if err := db.KVSet(ctx, "stale", []byte("v"), past); err != nil {
		t.Fatal(err)
	}
	_, ok, err := db.KVGet(ctx, "stale")
	if err != nil || ok {
		t.Fatalf("expected expired-miss, got ok=%v err=%v", ok, err)
	}
}

func TestKVPurgeExpiredRemovesRows(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).Unix()
	if err := db.KVSet(ctx, "pending:inject:1", []byte("old"), past); err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(ctx, "pending:inject:2", "new"); err != nil {
		t.Fatal(err)
	}
	if err := db.KVPurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM kv WHERE key = 'pending:inject:1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expired rows = %d", n)
	}
	if _, ok, err := db.KVGetString(ctx, "pending:inject:2"); err != nil || !ok {
		t.Fatalf("live key ok=%v err=%v", ok, err)
	}
}

func TestKVKeysWithPrefix(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	for k, v := range map[string]string{
		"target:1": "a",
		"target:2": "b",
		"other:1":  "c",
	} {
		if err := db.KVSetString(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := db.KVKeysWithPrefix(ctx, "target:")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"target:1", "target:2"}) {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestPairingSingleUse(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	tok := "abc123tokenvalue"
	if err := db.PutPairingToken(ctx, tok, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	ok, err := db.ConsumePairingToken(ctx, tok)
	if err != nil || !ok {
		t.Fatalf("first consume: ok=%v err=%v", ok, err)
	}
	ok, err = db.ConsumePairingToken(ctx, tok)
	if err != nil || ok {
		t.Fatalf("second consume must fail: ok=%v err=%v", ok, err)
	}
}

func TestPairingExpired(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	tok := "expiredtoken123"
	// directly insert an expired token to avoid waiting
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO pairing_tokens(token, created_at, expires_at, consumed) VALUES (?, ?, ?, 0)`,
		tok, time.Now().Add(-time.Hour).Unix(), time.Now().Add(-time.Minute).Unix())
	if err != nil {
		t.Fatal(err)
	}
	ok, err := db.ConsumePairingToken(ctx, tok)
	if err != nil || ok {
		t.Fatalf("expired consume must fail: ok=%v err=%v", ok, err)
	}
}

func TestPairingRaceOnlyOneWins(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	tok := "racetoken1234567"
	if err := db.PutPairingToken(ctx, tok, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var wins int
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := db.ConsumePairingToken(ctx, tok)
			if err != nil {
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", wins)
	}
}
