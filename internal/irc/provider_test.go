package irc

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

func TestProviderSendsApprovalToOwnerDM(t *testing.T) {
	c, serverConn := providerPipeClient(t)
	p := NewProvider(c, "owner")
	done := make(chan string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		done <- readTestLine(t, r)
	}()
	msgID, err := p.SendApproval(t.Context(), chatout.ApprovalRequest{
		ID:        "apr_1",
		SessionID: "s1",
		Agent:     "claude",
		Tool:      "Bash",
		InputJSON: `{"command":"pwd"}`,
		RiskLevel: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "apr_1" {
		t.Fatalf("msg id = %q", msgID)
	}
	select {
	case line := <-done:
		if !strings.HasPrefix(line, "PRIVMSG owner :Approval apr_1") || !strings.Contains(line, FormatOnibiCommand("approve", "apr_1")) {
			t.Fatalf("line = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("missing approval DM")
	}
}

func TestProviderRoutesInboundTextAndCommandDecisions(t *testing.T) {
	p := NewProvider(New("pipe", "onibi", "onibi", ""), "owner")
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	inbound := make(chan string, 1)
	if err := p.OnInboundText(func(text string, sender chatout.Sender) {
		inbound <- text + ":" + sender.ID + ":" + sender.ChannelID
	}); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan chatout.Decision, 1)
	if err := p.OnDecision("apr_1", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	if err := p.routeMessage(t.Context(), Message{Command: "PRIVMSG", Prefix: "owner!u@h", Params: []string{"onibi"}, Trailing: "pwd"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "pwd:owner:onibi" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing inbound")
	}
	if err := p.routeMessage(t.Context(), Message{Command: "PRIVMSG", Prefix: "owner!u@h", Params: []string{"onibi"}, Trailing: "!onibi dn apr_1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "deny" || got.MessageID != "apr_1" || got.Sender.ID != "owner" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision")
	}
	if len(audit) != 2 || audit[0].Kind != "provider.irc.text_in" || audit[1].Kind != "provider.irc.command" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderIgnoresNonOwnerAndNonDM(t *testing.T) {
	p := NewProvider(New("pipe", "onibi", "onibi", ""), "owner")
	inbound := make(chan string, 1)
	if err := p.OnInboundText(func(text string, _ chatout.Sender) { inbound <- text }); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []Message{
		{Command: "PRIVMSG", Prefix: "other!u@h", Params: []string{"onibi"}, Trailing: "pwd"},
		{Command: "PRIVMSG", Prefix: "owner!u@h", Params: []string{"#chan"}, Trailing: "pwd"},
	} {
		if err := p.routeMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case got := <-inbound:
		t.Fatalf("unexpected inbound = %q", got)
	default:
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	c, serverConn := providerPipeClient(t)
	p := NewProvider(c, "owner")
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	done := make(chan []string, 1)
	go func() {
		r := bufio.NewReader(serverConn)
		done <- []string{readTestLine(t, r), readTestLine(t, r)}
	}()
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", MessageChunkLimit+2))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	select {
	case lines := <-done:
		if len(strings.TrimPrefix(lines[0], "PRIVMSG owner :")) != MessageChunkLimit || !strings.HasSuffix(lines[1], "xx") {
			t.Fatalf("lines = %#v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("missing tail lines")
	}
	if len(audit) != 2 || audit[0].Kind != "provider.irc.tail_chunk" || audit[0].SessionID != "s1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderCapabilitiesAndRateLimit(t *testing.T) {
	p := NewProvider(nil, "owner")
	if p.Name() != "irc" || len(p.Capabilities()) != 6 {
		t.Fatalf("provider = %s %#v", p.Name(), p.Capabilities())
	}
	rl := p.RateLimit()
	if rl.PerSecond.Limit != 1 || rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
		t.Fatalf("rate limit = %#v", rl)
	}
}

func providerPipeClient(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = serverConn.Close() })
	c := New("pipe", "onibi", "onibi", "")
	c.SendPace = -1
	c.SetConnForTest(clientConn)
	t.Cleanup(func() { _ = c.Close() })
	return c, serverConn
}
