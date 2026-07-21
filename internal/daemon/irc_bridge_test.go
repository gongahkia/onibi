//go:build !onibi_remote

package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/chatout"
)

func TestConfigureIRCProviderRoutesOwnerInputAndDecisions(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	p := &ircBridgeStub{}
	if err := d.configureIRCProvider(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	p.inbound("/approve", chatout.Sender{ID: "owner-account"})
	if len(p.texts) != 1 || p.texts[0] != "Approval id required." {
		t.Fatalf("texts = %#v", p.texts)
	}
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	p.decision(chatout.Decision{ApprovalID: id, Verdict: "approve", Sender: chatout.Sender{ID: "owner-account"}})
	a, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateApproved || a.DecidedBy != ircActorID("owner-account") {
		t.Fatalf("approval = %#v", a)
	}
}

func TestIRCActorIDIsStableAndScoped(t *testing.T) {
	if got := ircActorID("Owner-Account"); got != ircActorID("owner-account") || got == ircActorID("other-account") || got == 0 {
		t.Fatalf("actor ids owner=%d other=%d", got, ircActorID("other-account"))
	}
}

func TestIRCTailChunksStripANSIAndApplyOutputPolicy(t *testing.T) {
	d := New(Options{ProviderOutput: ProviderOutputPolicy{MaxBytes: 64, Redaction: "default"}})
	chunks := make(chan []byte, 1)
	chunks <- []byte("\x1b[31mhello\x1b[0m")
	close(chunks)
	var got []byte
	for chunk := range d.ircTailChunks(t.Context(), chunks) {
		got = append(got, chunk...)
	}
	if text := string(got); text != "hello" || strings.Contains(text, "\x1b") {
		t.Fatalf("tail = %q", text)
	}
}

type ircBridgeStub struct {
	inbound  func(string, chatout.Sender)
	decision func(chatout.Decision)
	texts    []string
}

func (p *ircBridgeStub) Name() string                       { return "irc" }
func (p *ircBridgeStub) Capabilities() []chatout.Capability { return nil }
func (p *ircBridgeStub) SendApproval(context.Context, chatout.ApprovalRequest) (string, error) {
	return "", nil
}
func (p *ircBridgeStub) OnDecision(_ string, fn func(chatout.Decision)) error {
	p.decision = fn
	return nil
}
func (p *ircBridgeStub) SendText(_ context.Context, text string) error {
	p.texts = append(p.texts, text)
	return nil
}
func (p *ircBridgeStub) OnInboundText(fn func(string, chatout.Sender)) error {
	p.inbound = fn
	return nil
}
func (p *ircBridgeStub) TailStream(context.Context, string, <-chan []byte) error { return nil }
func (p *ircBridgeStub) Connect(context.Context) error                           { return nil }
func (p *ircBridgeStub) Reconnect(context.Context) error                         { return nil }
func (p *ircBridgeStub) Close() error                                            { return nil }
func (p *ircBridgeStub) RecordInteraction(context.Context, chatout.AuditInteraction) error {
	return nil
}
func (p *ircBridgeStub) RateLimit() chatout.RateLimitPolicy {
	return chatout.RateLimitPolicy{PerSecond: chatout.RateLimitBucket{Window: time.Second}}
}
