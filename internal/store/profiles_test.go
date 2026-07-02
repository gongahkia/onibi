package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProfileUpsertEncryptsPayload(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/profiles.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	profile := Profile{Name: "work", Transport: "tailscale", Agent: "claude", Workspace: "work-ws", CWD: "/tmp/work"}
	if err := db.ProfileUpsert(ctx, profile); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := db.SQL().QueryRowContext(ctx, `SELECT data_enc FROM profiles WHERE name = ?`, "work").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tailscale") || strings.Contains(string(raw), "claude") || strings.Contains(string(raw), "/tmp/work") {
		t.Fatalf("profile payload stored plaintext: %q", string(raw))
	}
	got, ok, err := db.ProfileGet(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("profile missing")
	}
	if got.Transport != "tailscale" || got.Agent != "claude" || got.Workspace != "work-ws" || got.CWD != "/tmp/work" {
		t.Fatalf("profile = %#v", got)
	}
}

func TestProfileListTouchAndRemove(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/profiles.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ProfileUpsert(ctx, Profile{Name: "a", Transport: "lan"}); err != nil {
		t.Fatal(err)
	}
	if err := db.ProfileUpsert(ctx, Profile{Name: "b", Transport: "tailscale"}); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(10, 0).UTC()
	if err := db.ProfileTouch(ctx, "b", when); err != nil {
		t.Fatal(err)
	}
	list, err := db.ProfileList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "b" || !list[0].LastUsedAt.Equal(when) {
		t.Fatalf("list = %#v", list)
	}
	removed, err := db.ProfileRemove(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("profile not removed")
	}
	_, ok, err := db.ProfileGet(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("removed profile still exists")
	}
}
