package gotify

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const ProviderMessageChunkLimit = 3800

var _ chatout.Provider = (*Provider)(nil)

type ApprovalURLFunc func(chatout.ApprovalRequest) (string, error)

type Provider struct {
	Client      *Client
	ApprovalURL ApprovalURLFunc
	Title       string
	Priority    int
	Audit       func(context.Context, chatout.AuditInteraction) error
}

func NewProvider(client *Client, approvalURL ApprovalURLFunc) *Provider {
	return &Provider{Client: client, ApprovalURL: approvalURL, Title: "Onibi", Priority: 8}
}

func (p *Provider) Name() string {
	return "gotify"
}

func (p *Provider) Capabilities() []chatout.Capability {
	return []chatout.Capability{
		chatout.CapabilityApprovalSend,
		chatout.CapabilityTextOut,
		chatout.CapabilityTailStream,
		chatout.CapabilityReconnect,
	}
}

func (p *Provider) SendApproval(ctx context.Context, req chatout.ApprovalRequest) (string, error) {
	if strings.TrimSpace(req.ID) == "" {
		return "", errors.New("approval id required")
	}
	clickURL, err := p.approvalURL(req)
	if err != nil {
		return "", err
	}
	msg := Message{
		Title:    p.title("Approval"),
		Message:  gotifyApprovalBody(req, clickURL),
		Priority: p.priority(),
		Extras:   ApprovalExtras(clickURL),
	}
	sent, err := p.client().SendMessage(ctx, msg)
	if err != nil {
		_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.gotify.send_error", SessionID: req.SessionID, Meta: map[string]any{"approval": req.ID, "err": err.Error()}})
		return "", err
	}
	msgID := gotifyMessageID(sent.ID)
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:      "provider.gotify.send",
		MessageID: msgID,
		SessionID: req.SessionID,
		Payload:   msg.Message,
		Meta:      map[string]any{"approval": req.ID, "click_url": clickURL != ""},
	}); err != nil {
		return "", err
	}
	return msgID, nil
}

func (p *Provider) OnDecision(string, func(chatout.Decision)) error {
	return errors.New("gotify decisions require signed action URL web handler")
}

func (p *Provider) SendText(ctx context.Context, text string) error {
	for i, chunk := range chatout.Chunks(text, ProviderMessageChunkLimit) {
		sent, err := p.client().SendMessage(ctx, Message{Title: p.title("Message"), Message: chunk, Priority: p.priority()})
		if err != nil {
			_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.gotify.text_error", Meta: map[string]any{"err": err.Error()}})
			return err
		}
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.gotify.text_out",
			MessageID: gotifyMessageID(sent.ID),
			Payload:   chunk,
			Meta:      map[string]any{"index": i, "bytes": len(chunk)},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) OnInboundText(func(string, chatout.Sender)) error {
	return errors.New("gotify inbound text unsupported")
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
			for i, chunk := range chatout.Chunks(string(b), ProviderMessageChunkLimit) {
				sent, err := p.client().SendMessage(ctx, Message{Title: p.title("Tail " + sessionID), Message: chunk, Priority: p.priority()})
				if err != nil {
					_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.gotify.tail_error", SessionID: sessionID, Meta: map[string]any{"err": err.Error()}})
					return err
				}
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.gotify.tail_chunk",
					MessageID: gotifyMessageID(sent.ID),
					SessionID: sessionID,
					Payload:   chunk,
					Meta:      map[string]any{"index": i, "bytes": len(chunk)},
				}); err != nil {
					return err
				}
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	client := p.client()
	if strings.TrimSpace(client.ClientToken) == "" {
		return errors.New("gotify client token required")
	}
	if err := client.Validate(ctx); err != nil {
		return err
	}
	return client.Tail(ctx, TailOptions{
		RetryMin: time.Second,
		RetryMax: 30 * time.Second,
		AfterError: func(err error, delay time.Duration, attempt int) {
			_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.gotify.reconnect", Meta: map[string]any{"attempt": attempt, "delay": delay.String(), "err": gotifyErrString(err)}})
		},
	}, func(msg StreamMessage) error {
		return p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.gotify.stream",
			MessageID: gotifyMessageID(msg.ID),
			Payload:   msg.Message,
			Meta:      map[string]any{"app_id": msg.AppID, "title": msg.Title, "priority": msg.Priority},
		})
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

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("", "", "")
	}
	return p.Client
}

func (p *Provider) approvalURL(req chatout.ApprovalRequest) (string, error) {
	if p == nil || p.ApprovalURL == nil {
		return "", errors.New("gotify approval url function required")
	}
	u, err := p.ApprovalURL(req)
	if err != nil {
		return "", err
	}
	u = strings.TrimSpace(u)
	if u == "" {
		return "", errors.New("gotify approval url required")
	}
	return u, nil
}

func (p *Provider) title(kind string) string {
	if p == nil {
		return strings.TrimSpace(kind)
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "Onibi"
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return title
	}
	return title + " " + kind
}

func (p *Provider) priority() int {
	if p == nil || p.Priority == 0 {
		return 8
	}
	return p.Priority
}

func gotifyApprovalBody(req chatout.ApprovalRequest, clickURL string) string {
	var b strings.Builder
	b.WriteString("Approval " + req.ID + "\nagent=" + req.Agent + " tool=" + req.Tool + " session=" + req.SessionID)
	if strings.TrimSpace(req.RiskLevel) != "" {
		b.WriteString("\nrisk=" + req.RiskLevel)
	}
	b.WriteString("\nopen=" + clickURL)
	if strings.TrimSpace(req.Diff) != "" {
		b.WriteString("\n\ndiff:\n" + trimGotifyBody(req.Diff))
	}
	if strings.TrimSpace(req.InputJSON) != "" {
		b.WriteString("\n\ninput:\n" + trimGotifyBody(req.InputJSON))
	}
	return b.String()
}

func trimGotifyBody(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 1800 {
		return text
	}
	return text[:1800] + "\n..."
}

func gotifyMessageID(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func gotifyErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
