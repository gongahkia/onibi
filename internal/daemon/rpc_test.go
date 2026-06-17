package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/pty"
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
