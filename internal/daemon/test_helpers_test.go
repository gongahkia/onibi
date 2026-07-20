package daemon

import (
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func openDaemonTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertAuditActions(t *testing.T, db *store.DB, want ...string) {
	t.Helper()
	rows, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Action] = true
	}
	for _, action := range want {
		if !seen[action] {
			t.Fatalf("missing audit action %s in %#v", action, rows)
		}
	}
}
