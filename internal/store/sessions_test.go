package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionLifecyclePersistsForRecovery(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	started := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := db.SessionUpsertStart(ctx, "s1", "work", "shell", "/tmp/work", "zsh -i", "tmux", "onibi-s1", started); err != nil {
		t.Fatal(err)
	}
	active, err := db.SessionsActive(ctx)
	if err != nil || len(active) != 1 {
		t.Fatalf("active=%#v err=%v", active, err)
	}
	if active[0].Name != "work" || active[0].TmuxTarget != "onibi-s1" || active[0].Ended {
		t.Fatalf("session=%#v", active[0])
	}
	touched := started.Add(30 * time.Second)
	if err := db.SessionTouch(ctx, "s1", touched); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionMarkEnded(ctx, "s1", touched.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	active, err = db.SessionsActive(ctx)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%#v err=%v", active, err)
	}
	recent, err := db.SessionsRecent(ctx, 10, true)
	if err != nil || len(recent) != 1 || !recent[0].Ended || !recent[0].LastActivity.Equal(touched) {
		t.Fatalf("recent=%#v err=%v", recent, err)
	}
}
