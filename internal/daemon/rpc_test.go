package daemon

import (
	"context"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestSessionControlRPCKillsViaControlSession(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{nil}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	d := New(Options{})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}

	resp, err := d.handleRPCRequest(context.Background(), intake.Event{Type: intake.TypeSessionControl, Session: "s1", Action: "kill"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "kill" || resp.SessionID != "s1" {
		t.Fatalf("response = %+v", resp)
	}
	if !containsCall(r.calls, "kill-session", "-t", "onibi-s1") || !s.Ended() {
		t.Fatalf("kill failed: calls=%#v ended=%v", r.calls, s.Ended())
	}
}
