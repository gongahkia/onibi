package irc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const commandPrefix = "!onibi"

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client     *Client
	OwnerNick  string
	OwnerToken string
	Audit      func(context.Context, chatout.AuditInteraction) error

	mu        sync.Mutex
	inbound   func(string, chatout.Sender)
	decisions map[string]func(chatout.Decision)
}

func NewProvider(client *Client, ownerNick, ownerToken string) *Provider {
	return &Provider{Client: client, OwnerNick: strings.TrimSpace(ownerNick), OwnerToken: strings.TrimSpace(ownerToken), decisions: map[string]func(chatout.Decision){}}
}

func (p *Provider) Name() string { return "irc" }

func (p *Provider) Capabilities() []chatout.Capability {
	return []chatout.Capability{chatout.CapabilityApprovalSend, chatout.CapabilityApprovalDecision, chatout.CapabilityTextOut, chatout.CapabilityTextIn, chatout.CapabilityTailStream, chatout.CapabilityReconnect}
}

func (p *Provider) SendApproval(ctx context.Context, req chatout.ApprovalRequest) (string, error) {
	if strings.TrimSpace(req.ID) == "" {
		return "", errors.New("approval id required")
	}
	text := fmt.Sprintf("Approval %s agent=%s tool=%s risk=%s. Reply: !onibi <token> /approve %s or /deny %s", req.ID, req.Agent, req.Tool, req.RiskLevel, req.ID, req.ID)
	if err := p.SendText(ctx, text); err != nil {
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
	p.decisions[key] = fn
	return nil
}

func (p *Provider) SendText(ctx context.Context, text string) error {
	if p == nil || p.Client == nil {
		return errors.New("irc provider client missing")
	}
	return p.Client.SendPrivmsg(ctx, p.OwnerNick, text)
}

func (p *Provider) OnInboundText(fn func(string, chatout.Sender)) error {
	if fn == nil {
		return errors.New("inbound callback required")
	}
	p.mu.Lock()
	p.inbound = fn
	p.mu.Unlock()
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
			if err := p.SendText(ctx, string(b)); err != nil {
				return err
			}
			if err := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.tail_chunk", SessionID: sessionID, Payload: string(b), Sender: chatout.Sender{ChannelID: p.OwnerNick}}); err != nil {
				return err
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	if p == nil || p.Client == nil {
		return errors.New("irc provider client missing")
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Client.Close()
		case <-done:
		}
	}()
	defer close(done)
	failures := 0
	for {
		if err := p.Client.Connect(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures++
			if err := sleepContext(ctx, ReconnectBackoff(failures)); err != nil {
				return err
			}
			continue
		}
		failures = 0
		err := p.Client.Run(ctx, p.route)
		_ = p.Client.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			failures++
		} else {
			failures = 1
		}
		if err := sleepContext(ctx, ReconnectBackoff(failures)); err != nil {
			return err
		}
	}
}

func (p *Provider) Reconnect(ctx context.Context) error { return p.Connect(ctx) }

func (p *Provider) Close() error {
	if p == nil || p.Client == nil {
		return nil
	}
	return p.Client.Close()
}

func (p *Provider) RecordInteraction(ctx context.Context, item chatout.AuditInteraction) error {
	if p.Audit == nil {
		return nil
	}
	return p.Audit(ctx, item)
}

func (p *Provider) RateLimit() chatout.RateLimitPolicy {
	return chatout.RateLimitPolicy{PerSecond: chatout.RateLimitBucket{Limit: 1, Burst: 1, Window: time.Second}, PerMinute: chatout.RateLimitBucket{Limit: 30, Burst: 1, Window: time.Minute}}
}

func (p *Provider) route(msg Message) error {
	if msg.Command != "PRIVMSG" || len(msg.Params) == 0 || !strings.EqualFold(msg.Params[0], p.Client.Config.Nick) {
		return nil
	}
	payload, ok := p.authorizedPayload(msg.Trailing)
	if !ok {
		return nil
	}
	sender := chatout.Sender{ID: msg.Nick(), DisplayName: msg.Nick(), ChannelID: msg.Nick()}
	if err := p.RecordInteraction(context.Background(), chatout.AuditInteraction{Kind: "provider.irc.text_in", Payload: payload, Sender: sender, Meta: map[string]any{"nick": msg.Nick()}}); err != nil {
		return err
	}
	if decision, ok := parseDecision(payload, sender); ok {
		p.dispatchDecision(decision.ApprovalID, decision)
		return nil
	}
	p.mu.Lock()
	fn := p.inbound
	p.mu.Unlock()
	if fn != nil {
		fn(payload, sender)
	}
	return nil
}

func (p *Provider) authorizedPayload(text string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 3)
	if len(parts) != 3 || !strings.EqualFold(parts[0], commandPrefix) || strings.TrimSpace(p.OwnerToken) == "" {
		return "", false
	}
	presented := sha256.Sum256([]byte(parts[1]))
	expected := sha256.Sum256([]byte(p.OwnerToken))
	if subtle.ConstantTimeCompare(presented[:], expected[:]) != 1 {
		return "", false
	}
	payload := strings.TrimSpace(parts[2])
	return payload, payload != ""
}

func parseDecision(payload string, sender chatout.Sender) (chatout.Decision, bool) {
	fields := strings.Fields(payload)
	if len(fields) < 2 {
		return chatout.Decision{}, false
	}
	verb := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	verdict := ""
	switch verb {
	case "approve", "ap":
		verdict = "approve"
	case "deny", "dn":
		verdict = "deny"
	default:
		return chatout.Decision{}, false
	}
	return chatout.Decision{ApprovalID: fields[1], Verdict: verdict, Sender: sender}, true
}

func (p *Provider) dispatchDecision(id string, decision chatout.Decision) {
	p.mu.Lock()
	fn := p.decisions[id]
	if fn == nil {
		fn = p.decisions["*"]
	}
	p.mu.Unlock()
	if fn != nil {
		fn(decision)
	}
}

func ReconnectBackoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := time.Second << min(failures-1, 6)
	if d > time.Minute {
		return time.Minute
	}
	return d
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
