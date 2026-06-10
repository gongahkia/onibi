package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAuditAppendAndRead(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "a.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	if err := db.AuditAppend(ctx, "approval.decided", "sess1", `{"x":1}`, 9999, "approved by user"); err != nil {
		t.Fatal(err)
	}
	if err := db.AuditAppend(ctx, "approval.decided", "sess1", `{"x":2}`, 9999, "denied by user"); err != nil {
		t.Fatal(err)
	}
	got, err := db.AuditRecent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries", len(got))
	}
	// newest first
	if got[0].Detail != "denied by user" {
		t.Fatalf("expected newest first, got %q", got[0].Detail)
	}
	if got[0].PayloadHash == "" {
		t.Fatal("expected hash, got empty")
	}
	n, _ := db.AuditCount(ctx)
	if n != 2 {
		t.Fatalf("count = %d", n)
	}
}

func TestAuditEmptyPayloadProducesEmptyHash(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "a.sqlite"))
	t.Cleanup(func() { _ = db.Close() })
	_ = db.AuditAppend(context.Background(), "session.start", "s", "", 0, "")
	got, _ := db.AuditRecent(context.Background(), 1)
	if got[0].PayloadHash != "" {
		t.Fatalf("expected empty hash for empty payload, got %q", got[0].PayloadHash)
	}
}
