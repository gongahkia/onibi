package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPromptQueueFIFOEditMoveCancel(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "p.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	p1, err := db.PromptEnqueue(ctx, "s1", 100, "one")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := db.PromptEnqueue(ctx, "s1", 100, "two")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Position != 1 || p2.Position != 2 {
		t.Fatalf("positions = %d %d", p1.Position, p2.Position)
	}
	edited, err := db.PromptUpdateText(ctx, p2.ID, "two edited")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Text != "two edited" {
		t.Fatalf("text = %q", edited.Text)
	}
	if _, err := db.PromptMove(ctx, p2.ID, 1); err != nil {
		t.Fatal(err)
	}
	next, ok, err := db.PromptNext(ctx, "s1")
	if err != nil || !ok {
		t.Fatalf("next ok=%v err=%v", ok, err)
	}
	if next.ID != p2.ID {
		t.Fatalf("next = %s want %s", next.ID, p2.ID)
	}
	if _, err := db.PromptSetState(ctx, p2.ID, PromptSent); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PromptSetState(ctx, p2.ID, PromptCancelled); err != ErrPromptNotQueued {
		t.Fatalf("cancel sent err = %v", err)
	}
	n, err := db.PromptCancelQueued(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancelled = %d", n)
	}
}

func TestPromptQueuePersistsAcrossOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.PromptEnqueue(context.Background(), "s1", 100, "persist")
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, ok, err := db.PromptNext(context.Background(), "s1")
	if err != nil || !ok {
		t.Fatalf("next ok=%v err=%v", ok, err)
	}
	if got.ID != p.ID || got.Text != "persist" {
		t.Fatalf("got = %#v want %s", got, p.ID)
	}
}
