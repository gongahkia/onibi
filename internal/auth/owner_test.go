package auth

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "a.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLoadOwnerUnset(t *testing.T) {
	db := openDB(t)
	_, err := LoadOwner(context.Background(), db)
	if err == nil || err.Error() != ErrOwnerNotSet.Error() {
		t.Fatalf("expected ErrOwnerNotSet, got %v", err)
	}
}

func TestSetAndCheckOwner(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	o := &Owner{}
	if err := SetOwner(ctx, db, o, 12345); err != nil {
		t.Fatal(err)
	}
	if !o.MustBeOwner(12345) {
		t.Fatal("expected owner check to pass for 12345")
	}
	if o.MustBeOwner(99999) {
		t.Fatal("expected owner check to fail for 99999")
	}
	// reload from store
	o2, err := LoadOwner(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if o2.ID() != 12345 {
		t.Fatalf("expected reload id 12345, got %d", o2.ID())
	}
}

func TestMustBeOwnerConcurrent(t *testing.T) {
	o := &Owner{}
	o.id.Store(42)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !o.MustBeOwner(42) {
				t.Error("concurrent check failed")
			}
		}()
	}
	wg.Wait()
}
