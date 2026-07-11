package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const providerReconnectMaxWait = 30 * time.Second

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client      *Client
	RoomID      string
	OwnerUserID string
	Timeout     time.Duration
	Audit       func(context.Context, chatout.AuditInteraction) error
	CryptoState *CryptoState
	PickleKey   []byte

	mu             sync.Mutex
	since          string
	inbound        func(string, chatout.Sender)
	decisions      map[string]func(chatout.Decision)
	approvalEvents map[string]string
}

func NewProvider(client *Client, roomID string) *Provider {
	return &Provider{
		Client:         client,
		RoomID:         strings.TrimSpace(roomID),
		decisions:      map[string]func(chatout.Decision){},
		approvalEvents: map[string]string{},
	}
}

func (p *Provider) Name() string {
	return "matrix"
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
	text := formatProviderApproval(req) + "\nReact ✅ to approve or ❌ to deny."
	events, err := p.client().SendTextEventChunks(ctx, p.roomID(), text)
	if err != nil {
		return "", err
	}
	first := ""
	p.mu.Lock()
	if p.approvalEvents == nil {
		p.approvalEvents = map[string]string{}
	}
	for _, ev := range events {
		if ev.EventID == "" {
			continue
		}
		if first == "" {
			first = ev.EventID
		}
		p.approvalEvents[ev.EventID] = req.ID
	}
	p.mu.Unlock()
	return first, nil
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
	return p.client().SendText(ctx, p.roomID(), text)
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
			events, err := p.client().SendTextEventChunks(ctx, p.roomID(), string(b))
			if err != nil {
				_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.matrix.tail_error", SessionID: sessionID, Meta: map[string]any{"room": p.roomID(), "err": err.Error()}})
				return err
			}
			for i, ev := range events {
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.matrix.tail_chunk",
					MessageID: ev.EventID,
					SessionID: sessionID,
					Payload:   ev.Body,
					Meta:      map[string]any{"room": p.roomID(), "event_id": ev.EventID, "index": i, "bytes": len(ev.Body)},
				}); err != nil {
					return err
				}
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	failures := 0
	for {
		timeout := p.Timeout
		if timeout <= 0 {
			timeout = 25 * time.Second
		}
		sync, err := p.client().SyncRoom(ctx, p.roomID(), p.nextSince(), timeout)
		if err != nil {
			failures++
			if err := chatout.Sleep(ctx, providerReconnectDelay(failures), p.client().Sleep); err != nil {
				return err
			}
			continue
		}
		failures = 0
		if sync.NextBatch != "" {
			p.setSince(sync.NextBatch)
		}
		room := sync.Rooms.Join[p.roomID()]
		for _, ev := range room.Timeline.Events {
			if err := p.routeEvent(ctx, ev); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (p *Provider) Reconnect(ctx context.Context) error {
	return p.Connect(ctx)
}

func (p *Provider) Close() error {
	return nil
}

func (p *Provider) RecordInteraction(ctx context.Context, item chatout.AuditInteraction) error {
	if p.Audit == nil {
		return nil
	}
	return p.Audit(ctx, item)
}

func (p *Provider) RateLimit() chatout.RateLimitPolicy {
	return chatout.RateLimitPolicy{
		PerSecond: chatout.RateLimitBucket{Window: time.Second},
		PerMinute: chatout.RateLimitBucket{Window: time.Minute},
	}
}

func (p *Provider) routeEvent(ctx context.Context, ev Event) error {
	if eventID, key, ok := Reaction(ev); ok {
		approvalID := p.approvalForEvent(eventID)
		verdict := providerReactionVerdict(key)
		if approvalID == "" || verdict == "" {
			return nil
		}
		sender := chatout.Sender{ID: ev.Sender, ChannelID: p.roomID()}
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.matrix.reaction",
			MessageID: eventID,
			Payload:   key,
			Sender:    sender,
			Meta:      map[string]any{"approval": approvalID, "reaction_event": ev.EventID},
		}); err != nil {
			return err
		}
		p.dispatchDecision(approvalID, eventID, chatout.Decision{ApprovalID: approvalID, Verdict: verdict, Sender: sender, MessageID: eventID})
		return nil
	}
	if ev.Type == EventRoomEncrypted {
		return p.routeEncryptedEvent(ctx, ev)
	}
	body := strings.TrimSpace(MessageBody(ev))
	if body == "" || (p.OwnerUserID != "" && ev.Sender != p.OwnerUserID) {
		return nil
	}
	sender := chatout.Sender{ID: ev.Sender, ChannelID: p.roomID()}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.matrix.text_in",
		Payload: body,
		Sender:  sender,
		Meta:    map[string]any{"room": p.roomID(), "event_id": ev.EventID},
	}); err != nil {
		return err
	}
	p.mu.Lock()
	fn := p.inbound
	p.mu.Unlock()
	if fn != nil {
		fn(body, sender)
	}
	return nil
}

func (p *Provider) routeEncryptedEvent(ctx context.Context, ev Event) error {
	if p == nil || p.CryptoState == nil || len(p.PickleKey) == 0 {
		return nil
	}
	var content MegolmEncryptedContent
	if err := json.Unmarshal(ev.Content, &content); err != nil || content.Algorithm != AlgorithmMegolmV1 {
		return nil
	}
	key, inbound, ok := megolmInboundSessionFor(*p.CryptoState, content.SessionID)
	if !ok {
		return nil
	}
	next, payload, _, err := DecryptMegolmRoomEvent(inbound, p.PickleKey, content, p.roomID())
	if err != nil {
		return p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.matrix.decrypt_error",
			MessageID: ev.EventID,
			Sender:    chatout.Sender{ID: ev.Sender, ChannelID: p.roomID()},
			Meta:      map[string]any{"room": p.roomID(), "session_id": content.SessionID, "err": err.Error()},
		})
	}
	if p.CryptoState.MegolmInboundSessions == nil {
		p.CryptoState.MegolmInboundSessions = map[string]MegolmInboundState{}
	}
	p.CryptoState.MegolmInboundSessions[key] = next
	if payload.Type == EventRoomEncrypted {
		return nil
	}
	return p.routeEvent(ctx, Event{EventID: ev.EventID, Type: payload.Type, Sender: ev.Sender, Content: payload.Content})
}

func megolmInboundSessionFor(state CryptoState, sessionID string) (string, MegolmInboundState, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", MegolmInboundState{}, false
	}
	for key, session := range state.MegolmInboundSessions {
		if session.SessionID == sessionID {
			return key, session, true
		}
	}
	return "", MegolmInboundState{}, false
}

func (p *Provider) dispatchDecision(approvalID, messageID string, decision chatout.Decision) {
	p.mu.Lock()
	exact := p.decisions[approvalID]
	byMessage := p.decisions[messageID]
	all := p.decisions["*"]
	p.mu.Unlock()
	switch {
	case exact != nil:
		exact(decision)
	case byMessage != nil:
		byMessage(decision)
	case all != nil:
		all(decision)
	}
}

func (p *Provider) approvalForEvent(eventID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.approvalEvents[eventID]
}

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("", "")
	}
	return p.Client
}

func (p *Provider) roomID() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RoomID)
}

func (p *Provider) nextSince() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.since
}

func (p *Provider) setSince(since string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.since = since
}

func providerReactionVerdict(key string) string {
	switch strings.TrimSpace(key) {
	case "✅", "👍":
		return "approve"
	case "❌", "👎":
		return "deny"
	default:
		return ""
	}
}

func providerReconnectDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	base := time.Second << attempt
	if base > providerReconnectMaxWait {
		base = providerReconnectMaxWait
	}
	return base + time.Duration(rand.Int63n(int64(base/2)+1))
}

func formatProviderApproval(req chatout.ApprovalRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Approval %s\nagent=%s tool=%s session=%s", req.ID, req.Agent, req.Tool, req.SessionID)
	if strings.TrimSpace(req.RiskLevel) != "" {
		fmt.Fprintf(&b, "\nrisk=%s", req.RiskLevel)
	}
	if strings.TrimSpace(req.Diff) != "" {
		fmt.Fprintf(&b, "\n\ndiff:\n%s", trimProviderBody(req.Diff))
	}
	if strings.TrimSpace(req.InputJSON) != "" {
		fmt.Fprintf(&b, "\n\ninput:\n%s", trimProviderBody(req.InputJSON))
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
