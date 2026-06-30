package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestSnapshotSaveListLoadDelete(t *testing.T) {
	db := openTemp(t)
	now := time.Now().UTC()
	err := db.SnapshotSave(context.Background(), SnapshotEntry{
		ID:               "snap1",
		SessionID:        "s1",
		Name:             "before-refactor",
		CreatedAt:        now,
		RingBuffer:       []byte("screen"),
		CWD:              "/tmp/repo",
		Env:              []string{"PATH=/bin", "A=1"},
		TranscriptOffset: 99,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := db.SnapshotsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "before-refactor" {
		t.Fatalf("list = %#v", list)
	}
	got, ok, err := db.SnapshotByName(context.Background(), "before-refactor")
	if err != nil || !ok {
		t.Fatalf("load ok=%v err=%v", ok, err)
	}
	if got.ID != "snap1" || got.SessionID != "s1" || string(got.RingBuffer) != "screen" || got.CWD != "/tmp/repo" || got.TranscriptOffset != 99 || !slices.Equal(got.Env, []string{"PATH=/bin", "A=1"}) {
		t.Fatalf("snapshot = %#v", got)
	}
	ok, err = db.SnapshotDeleteByName(context.Background(), "before-refactor")
	if err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
	_, ok, err = db.SnapshotByName(context.Background(), "before-refactor")
	if err != nil || ok {
		t.Fatalf("load after delete ok=%v err=%v", ok, err)
	}
}
