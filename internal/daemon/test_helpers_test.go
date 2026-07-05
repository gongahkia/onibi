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
