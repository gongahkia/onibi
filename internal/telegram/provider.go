package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const ProviderMessageChunkLimit = 3800

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client  *Client
	ChatID  int64
	Timeout int
	Audit   func(context.Context, chatout.AuditInteraction) error

	mu        sync.Mutex
	offset    int64
	inbound   func(string, chatout.Sender)
	decisions map[string]func(chatout.Decision)
}

func NewProvider(client *Client, chatID int64) *Provider {
	return &Provider{Client: client, ChatID: chatID, decisions: map[string]func(chatout.Decision){}}
}

func (p *Provider) Name() string {
	return "telegram"
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
	text := formatProviderApproval(req)
	msg, err := p.client().SendMessage(ctx, p.chatID(), text, &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
		{Text: "Approve", CallbackData: "ap:" + req.ID},
		{Text: "Deny", CallbackData: "dn:" + req.ID},
	}}})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(msg.MessageID, 10), nil
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
	for _, chunk := range ChunkText(text, ProviderMessageChunkLimit) {
		if _, err := p.client().SendMessage(ctx, p.chatID(), chunk, nil); err != nil {
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

func (p *Provider) TailStream(ctx context.Context, _ string, ch <-chan []byte) error {
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
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	if err := p.client().DeleteWebhook(ctx); err != nil {
		return err
	}
	failures := 0
	for {
		timeout := p.Timeout
		if timeout <= 0 {
			timeout = 25
		}
		updates, err := p.client().GetUpdates(ctx, p.nextOffset(), timeout)
		if err != nil {
			failures++
			if err := p.client().sleepRetry(ctx, ReconnectBackoff(failures)); err != nil {
				return err
			}
			continue
		}
		failures = 0
		for _, update := range updates {
			p.setOffset(update.UpdateID + 1)
			p.routeUpdate(ctx, update)
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
		PerSecond: chatout.RateLimitBucket{Limit: 30, Burst: 30, Window: time.Second},
		PerMinute: chatout.RateLimitBucket{Limit: 60, Burst: 60, Window: time.Minute},
	}
}

func (p *Provider) routeUpdate(ctx context.Context, update Update) {
	if update.Message != nil {
		if p.ChatID != 0 && update.Message.Chat.ID != p.ChatID {
			return
		}
		text := strings.TrimSpace(update.Message.Text)
		if text == "" {
			return
		}
		p.mu.Lock()
		fn := p.inbound
		p.mu.Unlock()
		if fn != nil {
			fn(text, senderFromMessage(update.Message))
		}
		return
	}
	if update.CallbackQuery == nil {
		return
	}
	q := update.CallbackQuery
	if q.Message == nil || (p.ChatID != 0 && q.Message.Chat.ID != p.ChatID) {
		_ = p.client().AnswerCallbackQuery(ctx, q.ID, "not authorized")
		return
	}
	action, approvalID, ok := strings.Cut(strings.TrimSpace(q.Data), ":")
	if !ok || approvalID == "" {
		_ = p.client().AnswerCallbackQuery(ctx, q.ID, "bad action")
		return
	}
	verdict := ""
	switch action {
	case "ap", "cf":
		verdict = "approve"
	case "dn":
		verdict = "deny"
	default:
		_ = p.client().AnswerCallbackQuery(ctx, q.ID, "unknown action")
		return
	}
	decision := chatout.Decision{
		ApprovalID: approvalID,
		Verdict:    verdict,
		Sender:     senderFromCallback(q),
		MessageID:  strconv.FormatInt(q.Message.MessageID, 10),
	}
	p.dispatchDecision(approvalID, decision)
	_ = p.client().AnswerCallbackQuery(ctx, q.ID, "ok")
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
		return NewClient("")
	}
	return p.Client
}

func (p *Provider) chatID() int64 {
	if p == nil {
		return 0
	}
	return p.ChatID
}

func (p *Provider) nextOffset() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.offset
}

func (p *Provider) setOffset(offset int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if offset > p.offset {
		p.offset = offset
	}
}

func senderFromMessage(msg *Message) chatout.Sender {
	s := chatout.Sender{ChannelID: strconv.FormatInt(msg.Chat.ID, 10)}
	if msg.From != nil {
		s.ID = strconv.FormatInt(msg.From.ID, 10)
		s.DisplayName = msg.From.Username
		if s.DisplayName == "" {
			s.DisplayName = strings.TrimSpace(msg.From.FirstName)
		}
	}
	return s
}

func senderFromCallback(q *CallbackQuery) chatout.Sender {
	channelID := ""
	if q.Message != nil {
		channelID = strconv.FormatInt(q.Message.Chat.ID, 10)
	}
	name := q.From.Username
	if name == "" {
		name = strings.TrimSpace(q.From.FirstName)
	}
	return chatout.Sender{ID: strconv.FormatInt(q.From.ID, 10), DisplayName: name, ChannelID: channelID}
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
	fmt.Fprintf(&b, "\n\nEdit: /edit %s <edited JSON>", req.ID)
	return b.String()
}

func trimProviderBody(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 1800 {
		return text
	}
	return text[:1800] + "\n..."
}
