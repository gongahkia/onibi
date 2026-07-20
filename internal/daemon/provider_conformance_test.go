//go:build !onibi_remote

package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
)

func TestChatProviderApprovalCommandConformance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		state   string
		verdict approval.Verdict
	}{
		{name: "approve", command: "/approve", state: approval.StateApproved, verdict: approval.VerdictApprove},
		{name: "deny", command: "deny", state: approval.StateDenied, verdict: approval.VerdictDeny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openDaemonTestDB(t)
			d := New(Options{DB: db})
			id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
			if err != nil {
				t.Fatal(err)
			}
			reply, err := d.handleProviderText(t.Context(), "", tc.command+" "+id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(reply, string(tc.verdict)) {
				t.Fatalf("reply = %q", reply)
			}
			got, err := d.Queue.Get(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.state {
				t.Fatalf("state = %s", got.State)
			}
			audit, err := db.AuditRecent(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(audit) != 1 || audit[0].Action != "approval.decided" || !strings.Contains(audit[0].Detail, "verdict="+string(tc.verdict)) {
				t.Fatalf("audit = %#v", audit)
			}
		})
	}
}

func TestNotifyProviderConformanceForwardsApprovalRequests(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	seen := make(chan *approval.Approval, 1)
	go d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		seen <- a
	})
	id, _, err := d.Queue.Request(ctx, "s1", "claude", "Write", `{"file_path":"/tmp/x"}`)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-seen:
		if got.ID != id || got.Tool != "Write" {
			t.Fatalf("approval = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval was not forwarded")
	}
}

func TestNotifyProviderConformanceReplaysPendingApprovalRequests(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	id, _, err := d.Queue.Request(ctx, "s1", "claude", "Write", `{"file_path":"/tmp/x"}`)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan *approval.Approval, 1)
	go d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		seen <- a
	})
	select {
	case got := <-seen:
		if got.ID != id || got.Tool != "Write" {
			t.Fatalf("approval = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending approval was not replayed")
	}
}
