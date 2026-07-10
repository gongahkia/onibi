package ntfy

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const ProviderMessageChunkLimit = 3800

var _ chatout.Provider = (*Provider)(nil)

type ApprovalActionsFunc func(chatout.ApprovalRequest) ([]Action, error)

type Provider struct {
	Client          *Client
	ApprovalActions ApprovalActionsFunc
	Title           string
	Tags            string
	Since           string
	Audit           func(context.Context, chatout.AuditInteraction) error
}

func NewProvider(client *Client, approvalActions ApprovalActionsFunc) *Provider {
	return &Provider{Client: client, ApprovalActions: approvalActions, Title: "Onibi", Tags: "warning", Since: "all"}
}

func (p *Provider) Name() string {
	return "ntfy"
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
	actions, err := p.approvalActions(req)
	if err != nil {
		return "", err
	}
	msg := Message{Title: p.title("approval"), Body: ntfyApprovalBody(req), Tags: p.tags(), Actions: actions}
	sent, err := p.client().PublishMessage(ctx, msg)
	if err != nil {
		_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.ntfy.send_error", SessionID: req.SessionID, Meta: map[string]any{"approval": req.ID, "err": err.Error()}})
		return "", err
	}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:      "provider.ntfy.send",
		MessageID: sent.ID,
		SessionID: req.SessionID,
		Payload:   msg.Body,
		Meta:      map[string]any{"approval": req.ID, "actions": len(actions)},
	}); err != nil {
		return "", err
	}
	return sent.ID, nil
}

func (p *Provider) OnDecision(string, func(chatout.Decision)) error {
	return errors.New("ntfy decisions require signed action URL web handler")
}

func (p *Provider) SendText(ctx context.Context, text string) error {
	for i, chunk := range chatout.Chunks(text, ProviderMessageChunkLimit) {
		sent, err := p.client().PublishMessage(ctx, Message{Title: p.title("message"), Body: chunk, Tags: p.tags()})
		if err != nil {
			_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.ntfy.text_error", Meta: map[string]any{"err": err.Error()}})
			return err
		}
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.ntfy.text_out",
			MessageID: sent.ID,
			Payload:   chunk,
			Meta:      map[string]any{"index": i, "bytes": len(chunk)},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) OnInboundText(func(string, chatout.Sender)) error {
	return errors.New("ntfy inbound text unsupported")
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
				sent, err := p.client().PublishMessage(ctx, Message{Title: p.title("tail " + sessionID), Body: chunk, Tags: p.tags()})
				if err != nil {
					_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.ntfy.tail_error", SessionID: sessionID, Meta: map[string]any{"err": err.Error()}})
					return err
				}
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.ntfy.tail_chunk",
					MessageID: sent.ID,
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
	return p.client().TailJSON(ctx, TailOptions{
		Since:    p.since(),
		RetryMin: time.Second,
		RetryMax: 30 * time.Second,
		AfterError: func(err error, delay time.Duration, attempt int) {
			_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.ntfy.reconnect", Meta: map[string]any{"attempt": attempt, "delay": delay.String(), "err": ntfyErrString(err)}})
		},
	}, func(msg StreamMessage) error {
		if msg.Event != "" && msg.Event != "message" {
			return nil
		}
		return p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.ntfy.stream",
			MessageID: msg.ID,
			Payload:   msg.Message,
			Meta:      map[string]any{"topic": msg.Topic, "title": msg.Title, "time": msg.Time},
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

func (p *Provider) approvalActions(req chatout.ApprovalRequest) ([]Action, error) {
	if p == nil || p.ApprovalActions == nil {
		return nil, errors.New("ntfy approval actions function required")
	}
	actions, err := p.ApprovalActions(req)
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, errors.New("ntfy approval actions required")
	}
	return actions, nil
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

func (p *Provider) tags() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Tags)
}

func (p *Provider) since() string {
	if p == nil {
		return "all"
	}
	return strings.TrimSpace(p.Since)
}

func ntfyApprovalBody(req chatout.ApprovalRequest) string {
	var b strings.Builder
	b.WriteString("Approval " + req.ID + "\nagent=" + req.Agent + " tool=" + req.Tool + " session=" + req.SessionID)
	if strings.TrimSpace(req.RiskLevel) != "" {
		b.WriteString("\nrisk=" + req.RiskLevel)
	}
	if strings.TrimSpace(req.Diff) != "" {
		b.WriteString("\n\ndiff:\n" + trimNtfyBody(req.Diff))
	}
	if strings.TrimSpace(req.InputJSON) != "" {
		b.WriteString("\n\ninput:\n" + trimNtfyBody(req.InputJSON))
	}
	return b.String()
}

func trimNtfyBody(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 1800 {
		return text
	}
	return text[:1800] + "\n..."
}

func ntfyErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
