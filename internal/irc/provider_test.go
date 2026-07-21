package irc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

func TestProviderRoutesOwnerTextAndApprovalCommands(t *testing.T) {
	p := NewProvider(nil, "onibi", "owner", "owner-account")
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	var texts []string
	if err := p.OnInboundText(func(text string, sender chatout.Sender) {
		texts = append(texts, text+":"+sender.ID)
	}); err != nil {
		t.Fatal(err)
	}
	var decisions []chatout.Decision
	if err := p.OnDecision("*", func(decision chatout.Decision) {
		decisions = append(decisions, decision)
	}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := p.route(ctx, Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"onibi", "pwd"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.route(ctx, Message{Prefix: "other!u@h", Command: "PRIVMSG", Params: []string{"onibi", "pwd"}, Tags: map[string]string{"account": "other-account"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.route(ctx, Message{Prefix: "other!u@h", Command: "PRIVMSG", Params: []string{"onibi", "pwd"}, Tags: map[string]string{"account": "owner-account"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.route(ctx, Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"onibi", "pwd"}, Tags: map[string]string{"account": "owner-account"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.route(ctx, Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"onibi", "!onibi approve apr_1"}, Tags: map[string]string{"account": "owner-account"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.route(ctx, Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"onibi", "!onibi deny apr_2"}, Tags: map[string]string{"account": "owner-account"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.WaitForOwner(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(texts, []string{"pwd:owner-account"}) {
		t.Fatalf("texts = %#v", texts)
	}
	if len(decisions) != 2 || decisions[0].ApprovalID != "apr_1" || decisions[0].Verdict != "approve" || decisions[1].ApprovalID != "apr_2" || decisions[1].Verdict != "deny" {
		t.Fatalf("decisions = %#v", decisions)
	}
	if len(audit) != 3 || audit[0].Kind != "provider.irc.text_in" || audit[1].Kind != "provider.irc.command" || audit[2].Kind != "provider.irc.command" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderBlocksOutboundBeforeAuthenticatedOwnerDM(t *testing.T) {
	p := NewProvider(NewClient(Config{Nick: "onibi", Username: "onibi", Password: "password"}), "onibi", "owner", "owner-account")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := p.SendText(ctx, "output"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("output error = %v", err)
	}
}

func TestProviderPreservesOrdinaryInputAndPacesOutbound(t *testing.T) {
	p := NewProvider(nil, "onibi", "owner", "owner-account")
	var inbound string
	if err := p.OnInboundText(func(text string, _ chatout.Sender) { inbound = text }); err != nil {
		t.Fatal(err)
	}
	if err := p.route(t.Context(), Message{Prefix: "owner!u@h", Command: "PRIVMSG", Params: []string{"onibi", "echo hi  "}, Tags: map[string]string{"account": "owner-account"}}); err != nil {
		t.Fatal(err)
	}
	if inbound != "echo hi  " {
		t.Fatalf("inbound = %q", inbound)
	}
	var sleeps []time.Duration
	p.Sleep = func(_ context.Context, delay time.Duration) error {
		sleeps = append(sleeps, delay)
		return nil
	}
	p.sendFn = func(context.Context, string) error { return nil }
	if err := p.SendText(t.Context(), "one"); err != nil {
		t.Fatal(err)
	}
	if err := p.SendText(t.Context(), "two"); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 1 || sleeps[0] < 900*time.Millisecond || sleeps[0] > time.Second {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestProviderTailChunksAndAudits(t *testing.T) {
	p := NewProvider(nil, "onibi", "owner", "owner-account")
	var sent []string
	p.sendFn = func(_ context.Context, text string) error {
		sent = append(sent, text)
		return nil
	}
	p.Sleep = func(context.Context, time.Duration) error { return nil }
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", ProviderMessageLimit+1))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || len([]rune(sent[0])) != ProviderMessageLimit || len([]rune(sent[1])) != 1 {
		t.Fatalf("sent = %#v", sent)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.irc.tail_chunk" || audit[0].SessionID != "s1" || audit[0].Payload != "" || audit[0].Meta["payload_sha256"] == "" {
		t.Fatalf("audit = %#v", audit)
	}
	policy := p.RateLimit()
	if policy.PerSecond.Limit != 1 || policy.PerSecond.Window != time.Second || policy.PerMinute.Limit != 30 {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestReconnectBackoffCapsAtSixtySeconds(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 60 * time.Second}
	for i, expected := range want {
		if got := ReconnectBackoff(i + 1); got != expected {
			t.Fatalf("failure %d = %s, want %s", i+1, got, expected)
		}
	}
}
