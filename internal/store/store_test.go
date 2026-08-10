package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kaypoh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(migrations) {
		t.Fatalf("migration count = %d, want %d", count, len(migrations))
	}
}
