package setup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "p.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNewTokenIsURLSafe(t *testing.T) {
	db := openDB(t)
	tok, err := NewToken(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 40 || len(tok) > 64 {
		t.Fatalf("unexpected token length %d", len(tok))
	}
	// base64url alphabet only (no padding)
	for _, c := range tok {
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			t.Fatalf("non-url-safe char %q in token %q", c, tok)
		}
	}
}

func TestConsumeFlow(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	tok, _ := NewToken(ctx, db)

	if err := Consume(ctx, db, tok); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := Consume(ctx, db, tok); !errors.Is(err, ErrPairExpired) {
		t.Fatalf("second consume should fail with ErrPairExpired, got %v", err)
	}
}

func TestConsumeUnknownToken(t *testing.T) {
	db := openDB(t)
	if err := Consume(context.Background(), db, "never-stored-this-token-payload"); !errors.Is(err, ErrPairExpired) {
		t.Fatalf("expected ErrPairExpired, got %v", err)
	}
}

func TestDeepLinkFormat(t *testing.T) {
	url := DeepLink("onibi_abcd_bot", "TOK123")
	if !strings.HasPrefix(url, "https://t.me/onibi_abcd_bot?start=pair_TOK123") {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestWebPairURLFormat(t *testing.T) {
	url := WebPairURL("https", "onibi.local", 8443, "TOK123")
	if url != "https://onibi.local:8443/pair/TOK123" {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestExtractToken(t *testing.T) {
	tok, ok := ExtractToken("pair_abc123")
	if !ok || tok != "abc123" {
		t.Fatalf("got tok=%q ok=%v", tok, ok)
	}
	if _, ok := ExtractToken("nopair"); ok {
		t.Fatal("expected false for malformed payload")
	}
}
