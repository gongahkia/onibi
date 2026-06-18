package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/telegram"
)

func TestRPCSessionInputWritesLiveSession(t *testing.T) {
	d := newApprovalDaemon(t)
	var got string
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		got += string(p)
		return len(p), nil
	}, nil, nil)
	s := NewSession("s1", "codex", "codex", host, 1024)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleRPCRequest(context.Background(), intake.Event{
		Type:    intake.TypeSessionInput,
		Session: "s1",
		Text:    "hello",
		Enter:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\n" {
		t.Fatalf("write = %q", got)
	}
	if !strings.Contains(resp.Text, "sent to codex") {
		t.Fatalf("response = %q", resp.Text)
	}
}

func TestRPCSessionPeekReturnsTail(t *testing.T) {
	d := newApprovalDaemon(t)
	s := NewSession("s1", "codex", "codex", nil, 1024)
	_, _ = s.Buf.Write([]byte("abcdef"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleRPCRequest(context.Background(), intake.Event{
		Type:    intake.TypeSessionPeek,
		Session: "s1",
		Limit:   3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "def" {
		t.Fatalf("peek = %q", resp.Text)
	}
}

func TestRPCPingReturnsDaemonHealth(t *testing.T) {
	d := newApprovalDaemon(t)
	resp, err := d.handleRPCRequest(context.Background(), intake.Event{Type: intake.TypePing})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pong", "uptime=", "sessions=0", "telegram_poller=ok"} {
		if !strings.Contains(resp.Text, want) {
			t.Fatalf("ping missing %q:\n%s", want, resp.Text)
		}
	}
}

func TestRPCDemoApprovalUsesApprovalQueue(t *testing.T) {
	d := newApprovalDaemon(t)
	d.Bot = telegram.NewMock(nil)
	ctx := context.Background()
	type result struct {
		resp intake.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := d.handleRPCRequest(ctx, intake.Event{Type: intake.TypeDemoApproval})
		done <- result{resp: resp, err: err}
	}()
	var pending []*approval.Approval
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		rows, err := d.Queue.Pending(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 {
			pending = rows
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) != 1 || pending[0].Agent != "demo" {
		t.Fatalf("pending = %#v", pending)
	}
	if err := d.Queue.Decide(ctx, pending[0].ID, approval.VerdictDeny, "", "test denied", 100); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.resp.Decision != string(approval.VerdictDeny) || got.resp.Reason != "test denied" {
			t.Fatalf("resp = %#v", got.resp)
		}
	case <-time.After(time.Second):
		t.Fatal("demo approval did not return")
	}
}
