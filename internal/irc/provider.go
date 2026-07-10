package irc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client    *Client
	OwnerNick string
	Audit     func(context.Context, chatout.AuditInteraction) error

	mu        sync.Mutex
	inbound   func(string, chatout.Sender)
	decisions map[string]func(chatout.Decision)
}

func NewProvider(client *Client, ownerNick string) *Provider {
	return &Provider{
		Client:    client,
		OwnerNick: strings.TrimSpace(ownerNick),
		decisions: map[string]func(chatout.Decision){},
	}
}

func (p *Provider) Name() string {
	return "irc"
}

func (p *Provider) Capabilities() []chatout.Capability {
	return []chatout.Capability{
		chatout.CapabilityApprovalSend,
		chatout.CapabilityApprovalDecision,
		chatout.CapabilityTextOut,
		chatout.CapabilityTextIn,
		chatout.CapabilityTailStream,
		chatout.CapabilityReconnect,
	}
}

func (p *Provider) SendApproval(ctx context.Context, req chatout.ApprovalRequest) (string, error) {
	if strings.TrimSpace(req.ID) == "" {
		return "", errors.New("approval id required")
	}
	text := formatProviderApproval(req) + "\nReply " + FormatOnibiCommand("approve", req.ID) + " or " + FormatOnibiCommand("deny", req.ID) + "."
	if err := p.client().SendPrivmsg(ctx, p.ownerNick(), text); err != nil {
		return "", err
	}
	return req.ID, nil
}

func (p *Provider) OnDecision(key string, fn func(chatout.Decision)) error {
	if fn == nil {
		return errors.New("decision callback required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "*"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.decisions == nil {
		p.decisions = map[string]func(chatout.Decision){}
	}
	p.decisions[key] = fn
	return nil
}

func (p *Provider) SendText(ctx context.Context, text string) error {
	return p.client().SendPrivmsg(ctx, p.ownerNick(), text)
}

func (p *Provider) OnInboundText(fn func(string, chatout.Sender)) error {
	if fn == nil {
		return errors.New("inbound callback required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inbound = fn
	return nil
}

func (p *Provider) TailStream(ctx context.Context, sessionID string, ch <-chan []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case b, ok := <-ch:
			if !ok {
				return nil
			}
			for i, chunk := range chatout.Chunks(sanitizeText(string(b)), MessageChunkLimit) {
				if err := p.client().SendPrivmsg(ctx, p.ownerNick(), chunk); err != nil {
					_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.tail_error", SessionID: sessionID, Meta: map[string]any{"nick": p.ownerNick(), "err": err.Error()}})
					return err
				}
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.irc.tail_chunk",
					SessionID: sessionID,
					Payload:   chunk,
					Sender:    chatout.Sender{ID: p.ownerNick(), ChannelID: p.client().Nick},
					Meta:      map[string]any{"nick": p.ownerNick(), "index": i, "bytes": len(chunk)},
				}); err != nil {
					return err
				}
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	prev := p.client().AfterError
	p.client().AfterError = func(err error, delay time.Duration, attempt int) {
		_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.reconnect", Meta: map[string]any{"attempt": attempt, "delay": delay.String(), "err": errString(err)}})
		if prev != nil {
			prev(err, delay, attempt)
		}
	}
	return p.client().RunWithReconnect(ctx, func(msg Message) error {
		return p.routeMessage(ctx, msg)
	})
}

func (p *Provider) Reconnect(ctx context.Context) error {
	return p.Connect(ctx)
}

func (p *Provider) Close() error {
	return p.client().Close()
}

func (p *Provider) RecordInteraction(ctx context.Context, item chatout.AuditInteraction) error {
	if p.Audit == nil {
		return nil
	}
	return p.Audit(ctx, item)
}

func (p *Provider) RateLimit() chatout.RateLimitPolicy {
	return chatout.RateLimitPolicy{
		PerSecond: chatout.RateLimitBucket{Limit: 1, Burst: 1, Window: time.Second},
		PerMinute: chatout.RateLimitBucket{Limit: 30, Burst: 30, Window: time.Minute},
	}
}

func (p *Provider) routeMessage(ctx context.Context, msg Message) error {
	if msg.Command != "PRIVMSG" || len(msg.Params) == 0 {
		return nil
	}
	if nick := strings.TrimSpace(p.client().Nick); nick != "" && !strings.EqualFold(msg.Params[0], nick) {
		return nil
	}
	from := msg.Nick()
	if owner := p.ownerNick(); owner != "" && !strings.EqualFold(from, owner) {
		return nil
	}
	text := strings.TrimSpace(msg.Trailing)
	if text == "" {
		return nil
	}
	sender := chatout.Sender{ID: from, DisplayName: from, ChannelID: msg.Params[0]}
	if action, id, ok := ParseOnibiCommand(text); ok {
		verdict := approvalVerdict(action)
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:    "provider.irc.command",
			Payload: text,
			Sender:  sender,
			Meta:    map[string]any{"action": action, "approval": id},
		}); err != nil {
			return err
		}
		if verdict != "" {
			p.dispatchDecision(id, chatout.Decision{ApprovalID: id, Verdict: verdict, Sender: sender, MessageID: id})
		}
		return nil
	}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.irc.text_in",
		Payload: text,
		Sender:  sender,
		Meta:    map[string]any{"nick": from},
	}); err != nil {
		return err
	}
	p.mu.Lock()
	fn := p.inbound
	p.mu.Unlock()
	if fn != nil {
		fn(text, sender)
	}
	return nil
}

func (p *Provider) dispatchDecision(approvalID string, decision chatout.Decision) {
	p.mu.Lock()
	exact := p.decisions[approvalID]
	all := p.decisions["*"]
	p.mu.Unlock()
	if exact != nil {
		exact(decision)
		return
	}
	if all != nil {
		all(decision)
	}
}

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("", "", "", "")
	}
	return p.Client
}

func (p *Provider) ownerNick() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.OwnerNick)
}

func approvalVerdict(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve", "ap":
		return "approve"
	case "deny", "dn":
		return "deny"
	default:
		return ""
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func formatProviderApproval(req chatout.ApprovalRequest) string {
	var b strings.Builder
	b.WriteString("Approval " + req.ID + "\nagent=" + req.Agent + " tool=" + req.Tool + " session=" + req.SessionID)
	if strings.TrimSpace(req.RiskLevel) != "" {
		b.WriteString("\nrisk=" + req.RiskLevel)
	}
	if strings.TrimSpace(req.Diff) != "" {
		b.WriteString("\n\ndiff:\n" + trimProviderBody(req.Diff))
	}
	if strings.TrimSpace(req.InputJSON) != "" {
		b.WriteString("\n\ninput:\n" + trimProviderBody(req.InputJSON))
	}
	return b.String()
}

func trimProviderBody(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 1800 {
		return text
	}
	return text[:1800] + "\n..."
}
