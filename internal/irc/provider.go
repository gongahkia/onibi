package irc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const (
	ProviderMessageLimit = 400
	ReconnectInitial     = time.Second
	ReconnectCap         = 60 * time.Second
)

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client    *Client
	Nick      string
	OwnerNick string
	Sleep     chatout.Sleeper
	Audit     func(context.Context, chatout.AuditInteraction) error

	mu        sync.Mutex
	inbound   func(string, chatout.Sender)
	decisions map[string]func(chatout.Decision)
	sendFn    func(context.Context, string) error
	rateMu    sync.Mutex
	nextSend  time.Time
	sentAt    []time.Time
}

func NewProvider(client *Client, nick, ownerNick string) *Provider {
	return &Provider{Client: client, Nick: strings.TrimSpace(nick), OwnerNick: strings.TrimSpace(ownerNick), decisions: map[string]func(chatout.Decision){}}
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
	text := fmt.Sprintf("Approval %s agent=%s tool=%s; !onibi approve %s or !onibi deny %s", req.ID, req.Agent, req.Tool, req.ID, req.ID)
	if err := p.sendText(ctx, text); err != nil {
		return "", err
	}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.approval", SessionID: req.SessionID, Payload: req.ID, Meta: map[string]any{"tool": req.Tool, "risk": req.RiskLevel}}); err != nil {
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
	return p.sendText(ctx, text)
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
		case body, ok := <-ch:
			if !ok {
				return nil
			}
			for i, chunk := range chatout.Chunks(string(body), ProviderMessageLimit) {
				if err := p.send(ctx, chunk); err != nil {
					return err
				}
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.tail_chunk", SessionID: sessionID, Payload: chunk, Sender: chatout.Sender{ID: p.OwnerNick, ChannelID: p.OwnerNick}, Meta: map[string]any{"index": i, "bytes": len(chunk)}}); err != nil {
					return err
				}
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	if p == nil || p.Client == nil {
		return errors.New("irc provider client required")
	}
	if strings.TrimSpace(p.Nick) == "" || strings.TrimSpace(p.OwnerNick) == "" {
		return errors.New("irc provider nick and owner nick are required")
	}
	if err := p.Client.Connect(ctx); err != nil {
		return err
	}
	for {
		msg, err := p.Client.Next(ctx)
		if err != nil {
			return err
		}
		if err := p.route(ctx, msg); err != nil {
			return err
		}
	}
}

func (p *Provider) Reconnect(ctx context.Context) error {
	if p == nil || p.Client == nil {
		return errors.New("irc provider client required")
	}
	for failures := 1; ; failures++ {
		_ = p.Close()
		err := p.Connect(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := chatout.Sleep(ctx, ReconnectBackoff(failures), p.Sleep); err != nil {
			return err
		}
	}
}

func (p *Provider) Close() error {
	if p == nil || p.Client == nil {
		return nil
	}
	return p.Client.Close()
}

func (p *Provider) RecordInteraction(ctx context.Context, item chatout.AuditInteraction) error {
	if p == nil || p.Audit == nil {
		return nil
	}
	if item.Payload != "" {
		sum := sha256.Sum256([]byte(item.Payload))
		if item.Meta == nil {
			item.Meta = map[string]any{}
		} else {
			meta := make(map[string]any, len(item.Meta)+1)
			for key, value := range item.Meta {
				meta[key] = value
			}
			item.Meta = meta
		}
		item.Meta["payload_sha256"] = hex.EncodeToString(sum[:])
		item.Payload = ""
	}
	return p.Audit(ctx, item)
}

func (p *Provider) RateLimit() chatout.RateLimitPolicy {
	return chatout.RateLimitPolicy{
		PerSecond: chatout.RateLimitBucket{Limit: 1, Burst: 1, Window: time.Second},
		PerMinute: chatout.RateLimitBucket{Limit: 30, Burst: 30, Window: time.Minute},
	}
}

func (p *Provider) route(ctx context.Context, msg Message) error {
	if msg.Command == "PING" {
		if p == nil || p.Client == nil {
			return errors.New("irc provider client required")
		}
		s, err := p.Client.currentSession()
		if err != nil {
			return err
		}
		return s.pong(ctx, msg)
	}
	if msg.Command != "PRIVMSG" || len(msg.Params) < 2 || !sameNick(msg.Params[0], p.Nick) {
		return nil
	}
	sender := chatout.Sender{ID: sourceNick(msg.Prefix), DisplayName: sourceNick(msg.Prefix), ChannelID: p.Nick}
	if sender.ID == "" || !sameNick(sender.ID, p.OwnerNick) {
		return nil
	}
	text := msg.Params[len(msg.Params)-1]
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if decision, ok := parseDecision(text, sender); ok {
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.command", MessageID: decision.ApprovalID, Payload: text, Sender: sender, Meta: map[string]any{"verdict": decision.Verdict}}); err != nil {
			return err
		}
		p.dispatchDecision(decision)
		return nil
	}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.irc.text_in", Payload: text, Sender: sender, Meta: map[string]any{"target": p.Nick}}); err != nil {
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

func (p *Provider) dispatchDecision(decision chatout.Decision) {
	p.mu.Lock()
	exact := p.decisions[decision.ApprovalID]
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

func (p *Provider) sendText(ctx context.Context, text string) error {
	for i, chunk := range chatout.Chunks(text, ProviderMessageLimit) {
		if i > 0 {
			if err := chatout.Sleep(ctx, time.Second, p.Sleep); err != nil {
				return err
			}
		}
		if err := p.send(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) send(ctx context.Context, text string) error {
	if err := p.waitSendLimit(ctx); err != nil {
		return err
	}
	if p != nil && p.sendFn != nil {
		return p.sendFn(ctx, text)
	}
	if p == nil || p.Client == nil {
		return errors.New("irc provider client required")
	}
	return p.Client.SendPrivmsg(ctx, p.OwnerNick, text)
}

func (p *Provider) waitSendLimit(ctx context.Context) error {
	if p == nil {
		return errors.New("irc provider nil")
	}
	p.rateMu.Lock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	first := 0
	for first < len(p.sentAt) && !p.sentAt[first].After(cutoff) {
		first++
	}
	p.sentAt = p.sentAt[first:]
	scheduled := now
	if p.nextSend.After(scheduled) {
		scheduled = p.nextSend
	}
	if len(p.sentAt) >= 30 {
		minuteReady := p.sentAt[0].Add(time.Minute)
		if minuteReady.After(scheduled) {
			scheduled = minuteReady
		}
	}
	p.sentAt = append(p.sentAt, scheduled)
	p.nextSend = scheduled.Add(time.Second)
	p.rateMu.Unlock()
	return chatout.Sleep(ctx, time.Until(scheduled), p.Sleep)
}

func ReconnectBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	delay := ReconnectInitial
	for i := 1; i < failures; i++ {
		if delay >= ReconnectCap/2 {
			return ReconnectCap
		}
		delay *= 2
	}
	if delay > ReconnectCap {
		return ReconnectCap
	}
	return delay
}

func parseDecision(text string, sender chatout.Sender) (chatout.Decision, bool) {
	parts := strings.Fields(text)
	if len(parts) != 3 || !strings.EqualFold(parts[0], "!onibi") {
		return chatout.Decision{}, false
	}
	verdict := strings.ToLower(parts[1])
	if verdict != "approve" && verdict != "deny" || strings.TrimSpace(parts[2]) == "" {
		return chatout.Decision{}, false
	}
	return chatout.Decision{ApprovalID: parts[2], Verdict: verdict, Sender: sender, MessageID: parts[2]}, true
}

func sourceNick(prefix string) string {
	nick, _, _ := strings.Cut(prefix, "!")
	return strings.TrimSpace(nick)
}

func sameNick(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
