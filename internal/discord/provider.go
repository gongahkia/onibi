package discord

import (
	"context"
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
	providerGatewayURL       = "wss://gateway.discord.gg/?v=10&encoding=json"
	providerApprovalPrefix   = "onibi:approval:"
	providerEditModalPrefix  = "onibi:approval_edit:"
	providerEditInputID      = "json"
	providerReconnectMaxWait = 30 * time.Second
)

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client     *Client
	ChannelID  string
	GatewayURL string
	Intents    int
	Allow      map[string]bool
	Audit      func(context.Context, chatout.AuditInteraction) error
	Dial       func(context.Context, string) (*websocket.Conn, error)

	mu               sync.Mutex
	conn             *websocket.Conn
	state            GatewayState
	inbound          func(string, chatout.Sender)
	decisions        map[string]func(chatout.Decision)
	approvals        map[string]chatout.ApprovalRequest
	approvalMessages map[string]string
	tailThreads      map[string]string
}

func NewProvider(client *Client, channelID string) *Provider {
	return &Provider{
		Client:           client,
		ChannelID:        strings.TrimSpace(channelID),
		decisions:        map[string]func(chatout.Decision){},
		approvals:        map[string]chatout.ApprovalRequest{},
		approvalMessages: map[string]string{},
		tailThreads:      map[string]string{},
	}
}

func (p *Provider) Name() string {
	return "discord"
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
	msg, err := p.client().CreateComponentsMessage(ctx, p.channelID(), providerApprovalComponents(req))
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	if p.approvals == nil {
		p.approvals = map[string]chatout.ApprovalRequest{}
	}
	if p.approvalMessages == nil {
		p.approvalMessages = map[string]string{}
	}
	p.approvals[req.ID] = req
	p.approvalMessages[req.ID] = msg.ID
	p.mu.Unlock()
	return msg.ID, nil
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
	return p.client().CreateMessageChunks(ctx, p.channelID(), text, nil)
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
	target := p.channelID()
	if sessionID != "" {
		var err error
		target, err = p.tailChannel(ctx, sessionID)
		if err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case b, ok := <-ch:
			if !ok {
				return nil
			}
			var auditErr error
			if err := p.client().CreateMessageChunks(ctx, target, string(b), func(i int, chunk string) {
				if auditErr != nil {
					return
				}
				auditErr = p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.discord.tail_chunk",
					SessionID: sessionID,
					Payload:   chunk,
					Meta:      map[string]any{"channel": target, "index": i, "bytes": len(chunk)},
				})
			}); err != nil {
				_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.discord.tail_error", SessionID: sessionID, Meta: map[string]any{"channel": target, "err": err.Error()}})
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
		connectURL := p.gatewayURL()
		if u, _, _, ok := p.state.Resume(connectURL); ok {
			connectURL = u
		}
		conn, err := p.dial(ctx, connectURL)
		if err != nil {
			if err := p.sleepReconnect(ctx, attempt); err != nil {
				return err
			}
			attempt++
			continue
		}
		p.setConn(conn)
		helloFrame, err := ReadFrame(ctx, conn)
		if err != nil {
			_ = p.Close()
			if err := p.sleepReconnect(ctx, attempt); err != nil {
				return err
			}
			attempt++
			continue
		}
		p.state.Observe(helloFrame)
		hello, _, _ := ParseHello(helloFrame)
		socketCtx, stop := context.WithCancel(ctx)
		if hello.HeartbeatInterval > 0 {
			go p.runHeartbeat(socketCtx, conn, time.Duration(hello.HeartbeatInterval)*time.Millisecond)
		}
		if _, sessionID, seq, ok := p.state.Resume(p.gatewayURL()); ok {
			err = SendResume(ctx, conn, p.client().Token, sessionID, seq)
		} else {
			err = SendIdentify(ctx, conn, p.client().Token, p.intents())
		}
		if err != nil {
			stop()
			_ = p.Close()
			if err := p.sleepReconnect(ctx, attempt); err != nil {
				return err
			}
			attempt++
			continue
		}
		err = p.runSocket(socketCtx, conn)
		stop()
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
		PerSecond: chatout.RateLimitBucket{Limit: 50, Burst: 50, Window: time.Second},
		PerMinute: chatout.RateLimitBucket{Window: time.Minute},
	}
}

func (p *Provider) runSocket(ctx context.Context, conn *websocket.Conn) error {
	for {
		frame, err := ReadFrame(ctx, conn)
		if err != nil {
			return err
		}
		reconnect, err := p.routeFrame(ctx, frame)
		if err != nil {
			return err
		}
		if reconnect {
			return nil
		}
	}
}

func (p *Provider) routeFrame(ctx context.Context, frame GatewayFrame) (bool, error) {
	p.state.Observe(frame)
	if HandleReconnect(frame) {
		return true, nil
	}
	if msg, ok, err := ParseMessage(frame); err != nil {
		return false, err
	} else if ok {
		return false, p.routeMessage(ctx, msg)
	}
	if in, ok, err := ParseInteraction(frame); err != nil {
		return false, err
	} else if ok {
		return false, p.routeInteraction(ctx, in)
	}
	return false, nil
}

func (p *Provider) routeMessage(ctx context.Context, msg MessageCreate) error {
	text := strings.TrimSpace(msg.Content)
	if text == "" || !p.allows(msg.ChannelID, msg.Author.ID) {
		return nil
	}
	sender := chatout.Sender{ID: msg.Author.ID, ChannelID: msg.ChannelID}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.discord.text_in",
		Payload: text,
		Sender:  sender,
		Meta:    map[string]any{"channel": msg.ChannelID, "user": msg.Author.ID},
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

func (p *Provider) routeInteraction(ctx context.Context, in Interaction) error {
	if action, id, ok := providerApprovalAction(in.Data.CustomID); ok {
		return p.routeApprovalInteraction(ctx, in, action, id)
	}
	if id, ok := providerEditModalID(in.Data.CustomID); ok {
		return p.routeEditSubmit(ctx, in, id)
	}
	text := InteractionText(in)
	if strings.EqualFold(in.Data.Name, "onibi") && text != "" {
		sender := chatout.Sender{ID: InteractionUserID(in), ChannelID: in.ChannelID}
		if !p.allows(in.ChannelID, sender.ID) {
			return nil
		}
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:    "provider.discord.text_in",
			Payload: text,
			Sender:  sender,
			Meta:    map[string]any{"interaction": in.ID, "user": sender.ID},
		}); err != nil {
			return err
		}
		p.mu.Lock()
		fn := p.inbound
		p.mu.Unlock()
		if fn != nil {
			fn(text, sender)
		}
		return p.client().RespondInteraction(ctx, in.ID, in.Token, "Slash command received.")
	}
	return nil
}

func (p *Provider) routeApprovalInteraction(ctx context.Context, in Interaction, action, id string) error {
	user := InteractionUserID(in)
	messageID := in.Message.ID
	sender := chatout.Sender{ID: user, ChannelID: in.ChannelID}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:      "provider.discord.button",
		MessageID: messageID,
		Payload:   in.Data.CustomID,
		Sender:    sender,
		Meta:      map[string]any{"action": action, "approval": id, "channel": in.ChannelID},
	}); err != nil {
		return err
	}
	switch action {
	case "approve", "deny":
		p.dispatchDecision(id, messageID, chatout.Decision{
			ApprovalID: id,
			Verdict:    action,
			Sender:     sender,
			MessageID:  messageID,
		})
		return p.client().RespondInteraction(ctx, in.ID, in.Token, "Approval "+id+": "+action+".")
	case "edit":
		if err := p.client().RespondInteractionModal(ctx, in.ID, in.Token, p.editModal(id)); err != nil {
			return err
		}
		return p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.discord.edit_modal",
			MessageID: messageID,
			Sender:    sender,
			Meta:      map[string]any{"approval": id, "user": user},
		})
	default:
		return p.client().RespondInteraction(ctx, in.ID, in.Token, "Unknown approval action.")
	}
}

func (p *Provider) routeEditSubmit(ctx context.Context, in Interaction, id string) error {
	edited := InteractionModalValue(in, providerEditInputID)
	sender := chatout.Sender{ID: InteractionUserID(in), ChannelID: in.ChannelID}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.discord.edit_submit",
		Payload: edited,
		Sender:  sender,
		Meta:    map[string]any{"approval": id, "user": sender.ID},
	}); err != nil {
		return err
	}
	p.dispatchDecision(id, in.Message.ID, chatout.Decision{
		ApprovalID:   id,
		Verdict:      "edit",
		UpdatedInput: edited,
		Sender:       sender,
		MessageID:    in.Message.ID,
	})
	return p.client().RespondInteraction(ctx, in.ID, in.Token, "Approval "+id+": edited.")
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

func (p *Provider) tailChannel(ctx context.Context, sessionID string) (string, error) {
	parent := p.channelID()
	p.mu.Lock()
	if p.tailThreads == nil {
		p.tailThreads = map[string]string{}
	}
	thread := p.tailThreads[sessionID]
	p.mu.Unlock()
	if thread != "" {
		return thread, nil
	}
	seed, err := p.client().CreateMessagePayload(ctx, parent, map[string]any{
		"content":          "Onibi tail for session " + sessionID,
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		if auditErr := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.discord.thread_error", SessionID: sessionID, Meta: map[string]any{"channel": parent, "err": err.Error()}}); auditErr != nil {
			return "", auditErr
		}
		return parent, nil
	}
	ch, err := p.client().StartThreadFromMessage(ctx, parent, seed.ID, "onibi-"+sessionID)
	if err != nil {
		if auditErr := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.discord.thread_error", SessionID: sessionID, Meta: map[string]any{"channel": parent, "message": seed.ID, "err": err.Error()}}); auditErr != nil {
			return "", auditErr
		}
		return parent, nil
	}
	p.mu.Lock()
	p.tailThreads[sessionID] = ch.ID
	p.mu.Unlock()
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.discord.thread", SessionID: sessionID, Meta: map[string]any{"channel": parent, "thread": ch.ID}}); err != nil {
		return "", err
	}
	return ch.ID, nil
}

func (p *Provider) runHeartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if p.state.AckOverdue(2 * interval) {
				_ = conn.Close(websocket.StatusGoingAway, "discord heartbeat ack timeout")
				return errors.New("discord heartbeat ack timeout")
			}
			if err := SendHeartbeat(ctx, conn, p.state.HeartbeatSeq()); err != nil {
				return err
			}
			p.state.MarkHeartbeatSent()
		}
	}
}

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("")
	}
	return p.Client
}

func (p *Provider) channelID() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.ChannelID)
}

func (p *Provider) gatewayURL() string {
	if p == nil || strings.TrimSpace(p.GatewayURL) == "" {
		return providerGatewayURL
	}
	return strings.TrimSpace(p.GatewayURL)
}

func (p *Provider) intents() int {
	if p == nil || p.Intents == 0 {
		return (1 << 9) | (1 << 12) | (1 << 15)
	}
	return p.Intents
}

func (p *Provider) dial(ctx context.Context, gatewayURL string) (*websocket.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, gatewayURL)
	}
	return DialGateway(ctx, gatewayURL)
}

func (p *Provider) setConn(conn *websocket.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conn = conn
}

func (p *Provider) sleepReconnect(ctx context.Context, attempt int) error {
	return chatout.Sleep(ctx, providerReconnectDelay(attempt), p.client().Sleep)
}

func (p *Provider) allows(channelID, userID string) bool {
	if len(p.Allow) == 0 {
		return true
	}
	return p.Allow[channelID] || p.Allow[userID]
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

func providerApprovalComponents(req chatout.ApprovalRequest) []any {
	text := formatProviderApproval(req)
	if len(text) > 1800 {
		text = text[:1800] + "\n..."
	}
	return []any{
		map[string]any{"type": 10, "content": "```" + text + "```"},
		map[string]any{"type": 1, "components": []any{
			map[string]any{"type": 2, "style": 3, "label": "Approve", "custom_id": providerApprovalPrefix + "approve:" + req.ID},
			map[string]any{"type": 2, "style": 4, "label": "Deny", "custom_id": providerApprovalPrefix + "deny:" + req.ID},
			map[string]any{"type": 2, "style": 2, "label": "Edit", "custom_id": providerApprovalPrefix + "edit:" + req.ID},
		}},
	}
}

func providerApprovalAction(customID string) (string, string, bool) {
	rest, ok := strings.CutPrefix(customID, providerApprovalPrefix)
	if !ok {
		return "", "", false
	}
	action, id, ok := strings.Cut(rest, ":")
	id = strings.TrimSpace(id)
	return action, id, ok && id != ""
}

func providerEditModalID(customID string) (string, bool) {
	id, ok := strings.CutPrefix(customID, providerEditModalPrefix)
	id = strings.TrimSpace(id)
	return id, ok && id != ""
}

func (p *Provider) editModal(approvalID string) map[string]any {
	req := p.approval(approvalID)
	return map[string]any{
		"custom_id": providerEditModalPrefix + approvalID,
		"title":     "Edit approval",
		"components": []any{map[string]any{
			"type": 1,
			"components": []any{map[string]any{
				"type":        4,
				"custom_id":   providerEditInputID,
				"style":       2,
				"label":       "JSON",
				"value":       req.InputJSON,
				"placeholder": "edited JSON",
				"required":    true,
			}},
		}},
	}
}

func (p *Provider) approval(id string) chatout.ApprovalRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.approvals[id]
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
