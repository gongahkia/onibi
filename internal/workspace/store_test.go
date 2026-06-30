package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestIndexEntryRoundtripsDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspaces")
	lastSeen := time.Date(2026, 6, 29, 10, 11, 12, 0, time.UTC)
	entry := IndexEntry{
		Name:             "project_1",
		Path:             "/Users/me/src/project",
		LastSeen:         lastSeen,
		SSHKey:           "keychain:onibi/project_1",
		DefaultTransport: "tailscale",
	}
	if err := SaveIndexEntry(dir, entry); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIndexEntry(dir, entry.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got != entry {
		t.Fatalf("entry = %#v, want %#v", got, entry)
	}
	path, err := EntryPath(dir, entry.Name)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != indexFilePerm {
		t.Fatalf("mode = %#o, want %#o", fi.Mode().Perm(), indexFilePerm)
	}
}

func TestDBStoreRoundtripsEncryptedPath(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	store, err := NewDBStore(db)
	if err != nil {
		t.Fatal(err)
	}
	entry := DBEntry{
		Name:      "alpha",
		Path:      "/Users/me/src/alpha",
		SSHKeyRef: "keychain:onibi/alpha",
		LastSeen:  time.Date(2026, 6, 29, 10, 11, 12, 0, time.UTC),
	}
	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Get(ctx, entry.Name)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got != entry {
		t.Fatalf("entry = %#v, want %#v", got, entry)
	}
	var raw []byte
	if err := db.SQL().QueryRowContext(ctx, `SELECT path_enc FROM workspaces WHERE name = ?`, entry.Name).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(entry.Path)) {
		t.Fatalf("encrypted path contains plaintext path")
	}
	if len(raw) == 0 {
		t.Fatal("encrypted path is empty")
	}
}

func TestDBStoreListsAndRemoves(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	store, err := NewDBStore(db)
	if err != nil {
		t.Fatal(err)
	}
	entries := []DBEntry{
		{Name: "older", Path: "/tmp/older", LastSeen: time.Unix(1, 0).UTC()},
		{Name: "newer", Path: "/tmp/newer", LastSeen: time.Unix(2, 0).UTC()},
	}
	for _, entry := range entries {
		if err := store.Upsert(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{list[0].Name, list[1].Name}
	if !slices.Equal(names, []string{"newer", "older"}) {
		t.Fatalf("names = %#v", names)
	}
	removed, err := store.Remove(ctx, "older")
	if err != nil || !removed {
		t.Fatalf("remove removed=%v err=%v", removed, err)
	}
	_, ok, err := store.Get(ctx, "older")
	if err != nil || ok {
		t.Fatalf("deleted get ok=%v err=%v", ok, err)
	}
}

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
