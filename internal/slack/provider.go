package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/chatout"
)

const (
	providerEditCallback    = "onibi_approval_edit"
	providerEditInputBlock  = "edited_input"
	providerEditInputAction = "json"
	providerReconnectMax    = 30 * time.Second
)

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client    *Client
	ChannelID string
	Allow     Allowlist
	Audit     func(context.Context, chatout.AuditInteraction) error
	Dial      func(context.Context, string) (*websocket.Conn, error)

	mu               sync.Mutex
	conn             *websocket.Conn
	inbound          func(string, chatout.Sender)
	decisions        map[string]func(chatout.Decision)
	approvals        map[string]chatout.ApprovalRequest
	approvalMessages map[string]string
}

func NewProvider(client *Client, channelID string) *Provider {
	return &Provider{
		Client:           client,
		ChannelID:        strings.TrimSpace(channelID),
		decisions:        map[string]func(chatout.Decision){},
		approvals:        map[string]chatout.ApprovalRequest{},
		approvalMessages: map[string]string{},
	}
}

func (p *Provider) Name() string {
	return "slack"
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
	resp, err := p.client().PostMessageBlocks(ctx, p.channelID(), "Onibi approval "+req.ID, providerApprovalBlocks(req))
	if err != nil {
		return "", err
	}
	msgID := providerMessageID(resp.Channel, resp.TS)
	p.mu.Lock()
	if p.approvals == nil {
		p.approvals = map[string]chatout.ApprovalRequest{}
	}
	if p.approvalMessages == nil {
		p.approvalMessages = map[string]string{}
	}
	p.approvals[req.ID] = req
	if msgID != "" {
		p.approvalMessages[req.ID] = msgID
	}
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
	return p.client().PostMessageChunks(ctx, p.channelID(), text, nil)
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
			var auditErr error
			if err := p.client().PostMessageChunks(ctx, p.channelID(), string(b), func(i int, chunk string) {
				if auditErr != nil {
					return
				}
				auditErr = p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.slack.tail_chunk",
					SessionID: sessionID,
					Payload:   chunk,
					Meta:      map[string]any{"channel": p.channelID(), "index": i, "bytes": len(chunk)},
				})
			}); err != nil {
				_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.slack.tail_error", SessionID: sessionID, Meta: map[string]any{"channel": p.channelID(), "err": err.Error()}})
				return err
			}
			if auditErr != nil {
				return auditErr
			}
		}
	}
}

func (p *Provider) Connect(ctx context.Context) error {
	attempt := 0
	for {
		socketURL, err := p.client().OpenSocket(ctx)
		if err != nil {
			return err
		}
		conn, err := p.dial(ctx, socketURL)
		if err != nil {
			if err := p.sleepReconnect(ctx, attempt); err != nil {
				return err
			}
			attempt++
			continue
		}
		p.setConn(conn)
		err = p.runSocket(ctx, conn)
		_ = p.Close()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err := p.sleepReconnect(ctx, attempt); err != nil {
			return err
		}
		attempt++
	}
}

func (p *Provider) Reconnect(ctx context.Context) error {
	return p.Connect(ctx)
}

func (p *Provider) Close() error {
	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.mu.Unlock()
	if conn != nil {
		conn.CloseNow()
	}
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
		PerSecond: chatout.RateLimitBucket{Limit: 1, Burst: 1, Window: time.Second},
		PerMinute: chatout.RateLimitBucket{Window: time.Minute},
	}
}

func (p *Provider) runSocket(ctx context.Context, conn *websocket.Conn) error {
	for {
		env, err := ReadEnvelope(ctx, conn)
		if err != nil {
			return err
		}
		payload, reconnect, err := p.routeEnvelope(ctx, env)
		if err != nil {
			payload = map[string]any{"text": err.Error()}
		}
		if !env.Accepts {
			payload = nil
		}
		if err := Ack(ctx, conn, env.EnvelopeID, payload); err != nil {
			return err
		}
		if reconnect {
			return nil
		}
	}
}

func (p *Provider) routeEnvelope(ctx context.Context, env Envelope) (any, bool, error) {
	switch env.Type {
	case "disconnect":
		return nil, ShouldReconnect(env), nil
	case "events_api":
		return nil, false, p.routeEvent(ctx, env)
	case "interactive":
		action, err := ParseInteraction(env)
		if err != nil {
			return nil, false, err
		}
		return p.routeInteraction(ctx, action)
	default:
		return nil, false, nil
	}
}

func (p *Provider) routeEvent(ctx context.Context, env Envelope) error {
	ev, err := ParseEvent(env)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(ev.Event.Text)
	if ev.Event.Type != "message" || text == "" || !p.Allow.Allows(ev.Event.Channel, ev.Event.User, ev.Event.ChannelType) {
		return nil
	}
	sender := chatout.Sender{ID: ev.Event.User, ChannelID: ev.Event.Channel}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.slack.text_in",
		Payload: text,
		Sender:  sender,
		Meta:    map[string]any{"channel": ev.Event.Channel, "user": ev.Event.User},
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

func (p *Provider) routeInteraction(ctx context.Context, action InteractionPayload) (any, bool, error) {
	if action.Type == "view_submission" {
		return nil, false, p.routeViewSubmission(ctx, action)
	}
	if len(action.Actions) == 0 {
		return nil, false, errors.New("slack action required")
	}
	raw := action.Actions[0]
	approvalID := providerApprovalID(raw.Value)
	sender := chatout.Sender{ID: action.User.ID, ChannelID: action.Channel.ID}
	messageID := providerMessageID(action.Channel.ID, action.Message.TS)
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:      "provider.slack.button",
		MessageID: messageID,
		Payload:   raw.Value,
		Sender:    sender,
		Meta:      map[string]any{"action": raw.ActionID, "approval": approvalID, "channel": action.Channel.ID},
	}); err != nil {
		return nil, false, err
	}
	switch strings.ToLower(raw.ActionID) {
	case "approve", "deny":
		p.dispatchDecision(approvalID, messageID, chatout.Decision{
			ApprovalID: approvalID,
			Verdict:    strings.ToLower(raw.ActionID),
			Sender:     sender,
			MessageID:  messageID,
		})
		return map[string]any{"text": "Approval " + approvalID + ": " + strings.ToLower(raw.ActionID) + "."}, false, nil
	case "edit":
		if _, err := p.client().OpenView(ctx, action.TriggerID, p.editModalView(approvalID)); err != nil {
			return nil, false, err
		}
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.slack.edit_modal",
			MessageID: messageID,
			Sender:    sender,
			Meta:      map[string]any{"approval": approvalID, "channel": action.Channel.ID},
		}); err != nil {
			return nil, false, err
		}
		return map[string]any{"text": "Edit modal opened for approval " + approvalID + "."}, false, nil
	default:
		return nil, false, errors.New("slack action invalid")
	}
}

func (p *Provider) routeViewSubmission(ctx context.Context, action InteractionPayload) error {
	if action.View.CallbackID != providerEditCallback {
		return nil
	}
	approvalID := strings.TrimSpace(action.View.PrivateMetadata)
	edited := ""
	if actions, ok := action.View.State.Values[providerEditInputBlock]; ok {
		if value, ok := actions[providerEditInputAction]; ok {
			edited = value.Value
		}
	}
	sender := chatout.Sender{ID: action.User.ID, ChannelID: action.Channel.ID}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.slack.edit_submit",
		Payload: edited,
		Sender:  sender,
		Meta:    map[string]any{"approval": approvalID},
	}); err != nil {
		return err
	}
	p.dispatchDecision(approvalID, action.View.ID, chatout.Decision{
		ApprovalID:   approvalID,
		Verdict:      "edit",
		UpdatedInput: edited,
		Sender:       sender,
		MessageID:    action.View.ID,
	})
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

func (p *Provider) editModalView(approvalID string) map[string]any {
	req := p.approval(approvalID)
	return map[string]any{
		"type":             "modal",
		"callback_id":      providerEditCallback,
		"private_metadata": approvalID,
		"title":            map[string]any{"type": "plain_text", "text": "Edit approval"},
		"submit":           map[string]any{"type": "plain_text", "text": "Submit"},
		"close":            map[string]any{"type": "plain_text", "text": "Cancel"},
		"blocks": []any{
			map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "Edit JSON for approval `" + approvalID + "`."}},
			map[string]any{
				"type":     "input",
				"block_id": providerEditInputBlock,
				"label":    map[string]any{"type": "plain_text", "text": "JSON"},
				"element": map[string]any{
					"type":          "plain_text_input",
					"action_id":     providerEditInputAction,
					"multiline":     true,
					"initial_value": req.InputJSON,
				},
			},
		},
	}
}

func (p *Provider) approval(id string) chatout.ApprovalRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.approvals[id]
}

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("", "")
	}
	return p.Client
}

func (p *Provider) channelID() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.ChannelID)
}

func (p *Provider) dial(ctx context.Context, socketURL string) (*websocket.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, socketURL)
	}
	return Dial(ctx, socketURL)
}

func (p *Provider) setConn(conn *websocket.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conn = conn
}

func (p *Provider) sleepReconnect(ctx context.Context, attempt int) error {
	return chatout.Sleep(ctx, providerReconnectDelay(attempt), p.client().Sleep)
}

func providerReconnectDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	base := time.Second << attempt
	if base > providerReconnectMax {
		base = providerReconnectMax
	}
	return base + time.Duration(rand.Int63n(int64(base/2)+1))
}

func providerApprovalBlocks(req chatout.ApprovalRequest) []any {
	text := formatProviderApproval(req)
	if len(text) > 2800 {
		text = text[:2800] + "\n..."
	}
	return []any{
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "```" + text + "```"}},
		map[string]any{"type": "actions", "block_id": "approval:" + req.ID, "elements": []any{
			map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Approve"},
				"style": "primary", "action_id": "approve", "value": providerApprovalValue(req, "approve"),
			},
			map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Deny"},
				"style": "danger", "action_id": "deny", "value": providerApprovalValue(req, "deny"),
			},
			map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Edit"},
				"action_id": "edit", "value": providerApprovalValue(req, "edit"),
			},
		}},
	}
}

func providerApprovalValue(req chatout.ApprovalRequest, verdict string) string {
	b, err := json.Marshal(map[string]string{
		"approval_id": req.ID,
		"session_id":  req.SessionID,
		"agent":       req.Agent,
		"tool":        req.Tool,
		"verdict":     verdict,
	})
	if err != nil {
		return req.ID
	}
	return string(b)
}

func providerApprovalID(value string) string {
	var payload struct {
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal([]byte(value), &payload); err == nil && payload.ApprovalID != "" {
		return payload.ApprovalID
	}
	return strings.TrimSpace(value)
}

func providerMessageID(channel, ts string) string {
	channel = strings.TrimSpace(channel)
	ts = strings.TrimSpace(ts)
	if channel == "" {
		return ts
	}
	if ts == "" {
		return channel
	}
	return channel + ":" + ts
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
