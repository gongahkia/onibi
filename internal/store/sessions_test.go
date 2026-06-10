package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStartRecentAndEnd(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	started := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := db.SessionUpsertStart(ctx, "s1", "claude", "claude", "/tmp", "claude --resume", "pty", "", started); err != nil {
		t.Fatal(err)
	}
	active, err := db.SessionsRecent(ctx, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "s1" || active[0].Command != "claude --resume" || active[0].Ended {
		t.Fatalf("active = %#v", active)
	}
	if err := db.SessionMarkEnded(ctx, "s1", time.Now()); err != nil {
		t.Fatal(err)
	}
	active, err = db.SessionsRecent(ctx, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active after end = %#v", active)
	}
	all, err := db.SessionsRecent(ctx, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !all[0].Ended {
		t.Fatalf("all = %#v", all)
	}
}
