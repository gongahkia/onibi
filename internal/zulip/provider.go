package zulip

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const (
	providerDefaultTopicPrefix = "onibi-"
	providerMessageChunkLimit  = 3800
)

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client      *Client
	Stream      string
	Topic       string
	TopicPrefix string
	OwnerEmail  string
	Audit       func(context.Context, chatout.AuditInteraction) error

	mu               sync.Mutex
	inbound          func(string, chatout.Sender)
	decisions        map[string]func(chatout.Decision)
	approvalMessages map[string]string
}

func NewProvider(client *Client, stream, topic string) *Provider {
	return &Provider{
		Client:           client,
		Stream:           strings.TrimSpace(stream),
		Topic:            strings.TrimSpace(topic),
		decisions:        map[string]func(chatout.Decision){},
		approvalMessages: map[string]string{},
	}
}

func (p *Provider) Name() string {
	return "zulip"
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
	topic := p.topicForSession(req.SessionID)
	text := formatProviderApproval(req) + "\n\nReply `approve " + req.ID + "` or `deny " + req.ID + "` in this topic."
	resp, err := p.client().SendStreamMessage(ctx, StreamMessage{Stream: p.stream(), Topic: topic, Content: text})
	if err != nil {
		return "", err
	}
	msgID := strconv.FormatInt(resp.ID, 10)
	p.mu.Lock()
	if p.approvalMessages == nil {
		p.approvalMessages = map[string]string{}
	}
	p.approvalMessages[req.ID] = msgID
	p.mu.Unlock()
	return msgID, nil
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
	for _, chunk := range chatout.Chunks(text, providerMessageChunkLimit) {
		if _, err := p.client().SendStreamMessage(ctx, StreamMessage{Stream: p.stream(), Topic: p.topic(), Content: chunk}); err != nil {
			return err
		}
	}
	return nil
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
	topic := p.topicForSession(sessionID)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case b, ok := <-ch:
			if !ok {
				return nil
			}
			for i, chunk := range chatout.Chunks(string(b), providerMessageChunkLimit) {
				resp, err := p.client().SendStreamMessage(ctx, StreamMessage{Stream: p.stream(), Topic: topic, Content: chunk})
				if err != nil {
					_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.zulip.tail_error", SessionID: sessionID, Meta: map[string]any{"topic": topic, "err": err.Error()}})
					return err
				}
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.zulip.tail_chunk",
					MessageID: strconv.FormatInt(resp.ID, 10),
					SessionID: sessionID,
					Payload:   chunk,
					Meta:      map[string]any{"topic": topic, "index": i, "bytes": len(chunk)},
				}); err != nil {
					return err
				}
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	return p.client().TailEvents(ctx, TailOptions{
		QueueOptions: QueueOptions{
			EventTypes: []string{"message"},
			Narrow:     [][]string{{"channel", p.stream()}},
		},
		RetryMax: 30 * time.Second,
		AfterError: func(err error, delay time.Duration, attempt int) {
			_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.zulip.reconnect", Meta: map[string]any{"attempt": attempt, "delay": delay.String(), "err": errString(err)}})
		},
	}, func(ev Event) error {
		return p.routeEvent(ctx, ev)
	})
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
	if ev.Message == nil || ev.Type != "message" {
		return nil
	}
	msg := ev.Message
	if msg.Type != "" && msg.Type != "stream" && msg.Type != "channel" {
		return nil
	}
	if owner := strings.TrimSpace(p.OwnerEmail); owner != "" && !strings.EqualFold(owner, msg.SenderEmail) {
		return nil
	}
	if bot := strings.TrimSpace(p.client().Email); bot != "" && strings.EqualFold(bot, msg.SenderEmail) {
		return nil
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return nil
	}
	topic := msg.Topic()
	sender := chatout.Sender{ID: msg.SenderEmail, DisplayName: msg.SenderFullName, ChannelID: topic}
	if action, id, ok := parseDecision(text); ok {
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.zulip.button",
			MessageID: strconv.FormatInt(msg.ID, 10),
			Payload:   text,
			Sender:    sender,
			Meta:      map[string]any{"action": action, "approval": id, "topic": topic},
		}); err != nil {
			return err
		}
		p.dispatchDecision(id, strconv.FormatInt(msg.ID, 10), chatout.Decision{ApprovalID: id, Verdict: action, Sender: sender, MessageID: strconv.FormatInt(msg.ID, 10)})
		return nil
	}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.zulip.text_in",
		Payload: text,
		Sender:  sender,
		Meta:    map[string]any{"topic": topic, "message": msg.ID},
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

func (p *Provider) dispatchDecision(approvalID, messageID string, decision chatout.Decision) {
	p.mu.Lock()
	exact := p.decisions[approvalID]
	byMessage := p.decisions[messageID]
	byStoredMessage := p.decisions[p.approvalMessages[approvalID]]
	all := p.decisions["*"]
	p.mu.Unlock()
	switch {
	case exact != nil:
		exact(decision)
	case byMessage != nil:
		byMessage(decision)
	case byStoredMessage != nil:
		byStoredMessage(decision)
	case all != nil:
		all(decision)
	}
}

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("", "", "")
	}
	return p.Client
}

func (p *Provider) stream() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Stream)
}

func (p *Provider) topic() string {
	if p == nil {
		return ""
	}
	if topic := strings.TrimSpace(p.Topic); topic != "" {
		return topic
	}
	return "onibi"
}

func (p *Provider) topicForSession(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return p.topic()
	}
	prefix := strings.TrimSpace(p.TopicPrefix)
	if prefix == "" {
		prefix = providerDefaultTopicPrefix
	}
	return prefix + sessionID
}

func parseDecision(text string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return "", "", false
	}
	action := strings.ToLower(fields[0])
	if action != "approve" && action != "deny" {
		return "", "", false
	}
	id := strings.TrimSpace(fields[1])
	return action, id, id != ""
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
