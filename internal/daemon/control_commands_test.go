package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestFleetControlDuplicateDoesNotReplay(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "control.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runner := &tmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: db})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(fleet.ControlPayload{SessionID: s.ID})
	if err != nil {
		t.Fatal(err)
	}
	control := fleet.Control{Version: fleet.ProtocolVersion, ID: "control-dup", OwnerID: "owner-1", HostID: "host-1", Command: "interrupt", Payload: payload, ExpiresAt: time.Now().UTC().Add(time.Minute)}
	first := d.handleFleetControl(context.Background(), control)
	second := d.handleFleetControl(context.Background(), control)
	if first.State != fleet.CommandSucceeded || second.State != fleet.CommandSucceeded {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	count := 0
	for _, call := range runner.calls {
		if containsCall([][]string{call}, "send-keys", "-t", "onibi-s1", "C-c") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("interrupt calls=%d all=%#v", count, runner.calls)
	}
}

func TestFleetControlLinuxHandoverReturnsManualAttach(t *testing.T) {
	setTerminalGOOS(t, "linux")
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "control.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, TerminalDefault: "auto"})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(fleet.ControlPayload{SessionID: s.ID, Target: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	result := d.handleFleetControl(context.Background(), fleet.Control{Version: fleet.ProtocolVersion, ID: "control-linux", OwnerID: "owner-1", HostID: "host-1", Command: "handover", Payload: payload, ExpiresAt: time.Now().UTC().Add(time.Minute)})
	if result.State != fleet.CommandSucceeded || result.Error != "" || !strings.Contains(result.Result, "tmux attach-session -t onibi-s1") {
		t.Fatalf("result=%#v", result)
	}
}

func TestStartupExpiresTimedOutControlCommand(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "control.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	_, created, err := db.ControlCommandCreate(context.Background(), store.ControlCommand{ID: "control-timeout", HostID: "host-1", SessionID: "s1", Action: "interrupt", State: fleet.CommandPending, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second)})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	d := New(Options{DB: db, SkipRestore: true})
	d.runStartupMaintenance(context.Background())
	command, err := db.ControlCommand(context.Background(), "control-timeout")
	if err != nil || command.State != fleet.CommandTimedOut || command.Result != "command timed out" {
		t.Fatalf("command=%#v err=%v", command, err)
	}
}
