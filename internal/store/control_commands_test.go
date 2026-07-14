package store

import (
	"context"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestControlCommandPersistsPayloadAndTerminalState(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	input := []byte(`{"session_id":"s1","input":"echo secret"}`)
	command, created, err := db.ControlCommandCreate(ctx, ControlCommand{ID: "control-1", HostID: "host-1", SessionID: "s1", Action: "input", Payload: input, State: fleet.CommandPending, CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil || !created {
		t.Fatalf("command=%#v created=%v err=%v", command, created, err)
	}
	if _, created, err := db.ControlCommandCreate(ctx, command); err != nil || created {
		t.Fatalf("duplicate created=%v err=%v", created, err)
	}
	if applied, err := db.ControlCommandComplete(ctx, command.ID, fleet.CommandSucceeded, "", now.Add(time.Second)); err != nil || !applied {
		t.Fatalf("complete applied=%v err=%v", applied, err)
	}
	if applied, err := db.ControlCommandComplete(ctx, command.ID, fleet.CommandSucceeded, "", now.Add(2*time.Second)); err != nil || applied {
		t.Fatalf("duplicate complete applied=%v err=%v", applied, err)
	}
	stored, err := db.ControlCommand(ctx, command.ID)
	if err != nil || stored.State != fleet.CommandSucceeded || string(stored.Payload) != string(input) || stored.CompletedAt.IsZero() {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if _, _, err := db.ControlCommandCreate(ctx, ControlCommand{ID: command.ID, HostID: "host-1", SessionID: "s1", Action: "input", Payload: []byte(`{"session_id":"s1","input":"other"}`), State: fleet.CommandPending, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}); err == nil {
		t.Fatal("expected command id collision")
	}
}

func TestControlCommandsExpireWithoutReplay(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	_, created, err := db.ControlCommandCreate(ctx, ControlCommand{ID: "control-timeout", HostID: "host-1", SessionID: "s1", Action: "interrupt", State: fleet.CommandPending, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second)})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if n, err := db.ControlCommandsExpire(ctx, now); err != nil || n != 1 {
		t.Fatalf("expired=%d err=%v", n, err)
	}
	if n, err := db.ControlCommandsExpire(ctx, now.Add(time.Second)); err != nil || n != 0 {
		t.Fatalf("second expired=%d err=%v", n, err)
	}
	stored, err := db.ControlCommand(ctx, "control-timeout")
	if err != nil || stored.State != fleet.CommandTimedOut || stored.Result != "command timed out" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if _, _, err := db.ControlCommandCreate(ctx, ControlCommand{ID: "bad", HostID: "host-1", SessionID: "s1", Action: "interrupt", State: fleet.CommandPending, CreatedAt: now, ExpiresAt: now}); err == nil {
		t.Fatal("expected invalid command")
	}
}
