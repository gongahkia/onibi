//go:build !onibi_remote

package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/chatout"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestIRCBridgeTextRoutesToTmuxTarget(t *testing.T) {
	runner := &tmuxRunner{results: [][]byte{nil, nil, []byte("$ pwd\n/tmp/onibi\n"), []byte("$ pwd\n/tmp/onibi\n"), []byte("$ pwd\n/tmp/onibi\n")}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = old })
	d := New(Options{DB: openDaemonTestDB(t)})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b := &ircBridge{d: d, provider: irc.NewProvider(nil, "owner", strings.Repeat("a", 32)), lastTail: map[string]string{}}
	b.handleText(context.Background(), "pwd", chatout.Sender{ID: "owner"})
	if !containsCall(runner.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "pwd") || !containsCall(runner.calls, "send-keys", "-t", "onibi-s1", "Enter") {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestIRCBridgeApprovalDecisionIsIdempotent(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	b := &ircBridge{d: d, provider: irc.NewProvider(nil, "owner", strings.Repeat("a", 32)), lastTail: map[string]string{}}
	b.handleDecision(context.Background(), chatout.Decision{ApprovalID: id, Verdict: "deny", Sender: chatout.Sender{ID: "owner"}})
	b.handleDecision(context.Background(), chatout.Decision{ApprovalID: id, Verdict: "deny", Sender: chatout.Sender{ID: "owner"}})
	a, err := d.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateDenied {
		t.Fatalf("state = %s", a.State)
	}
}

func TestIRCBridgeAuditHashesRedactedTail(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, IRCOwnerNick: "owner"})
	b := &ircBridge{d: d, provider: irc.NewProvider(nil, "owner", strings.Repeat("a", 32)), lastTail: map[string]string{}}
	secret := "sk-" + strings.Repeat("x", 20)
	b.sendOutput(context.Background(), "s1", "token="+secret)
	entries, err := db.AuditRecent(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "provider.irc.tail_chunk" || entries[0].PayloadHash == "" || strings.Contains(entries[0].Detail, secret) {
		t.Fatalf("audit = %#v", entries)
	}
}
