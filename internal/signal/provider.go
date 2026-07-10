package signal

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const ProviderMessageChunkLimit = 3800

var _ chatout.Provider = (*Provider)(nil)

type Provider struct {
	Client     *Client
	Recipients []string
	GroupID    string
	Owner      string
	Audit      func(context.Context, chatout.AuditInteraction) error

	mu        sync.Mutex
	inbound   func(string, chatout.Sender)
	decisions map[string]func(chatout.Decision)
	approvals map[int64]string
}

func NewProvider(client *Client, recipients []string, groupID string) *Provider {
	return &Provider{
		Client:     client,
		Recipients: cleanRecipients(recipients),
		GroupID:    strings.TrimSpace(groupID),
		decisions:  map[string]func(chatout.Decision){},
		approvals:  map[int64]string{},
	}
}

func (p *Provider) Name() string {
	return "signal"
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
	text := formatProviderApproval(req) + "\n\nReact \U0001f44d to approve or \U0001f44e to deny."
	sent, err := p.client().Send(ctx, SendRequest{Recipients: p.recipients(), GroupID: p.groupID(), Message: text})
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	if p.approvals == nil {
		p.approvals = map[int64]string{}
	}
	p.approvals[sent.Timestamp] = req.ID
	p.mu.Unlock()
	return strconv.FormatInt(sent.Timestamp, 10), nil
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
	for _, chunk := range chatout.Chunks(text, ProviderMessageChunkLimit) {
		if _, err := p.client().Send(ctx, SendRequest{Recipients: p.recipients(), GroupID: p.groupID(), Message: chunk}); err != nil {
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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case b, ok := <-ch:
			if !ok {
				return nil
			}
			for i, chunk := range chatout.Chunks(string(b), ProviderMessageChunkLimit) {
				sent, err := p.client().Send(ctx, SendRequest{Recipients: p.recipients(), GroupID: p.groupID(), Message: chunk})
				if err != nil {
					_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.signal.tail_error", SessionID: sessionID, Meta: map[string]any{"err": err.Error()}})
					return err
				}
				if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
					Kind:      "provider.signal.tail_chunk",
					MessageID: strconv.FormatInt(sent.Timestamp, 10),
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
	if err := p.client().Check(ctx); err != nil {
		return err
	}
	if _, err := p.client().SubscribeReceive(ctx); err != nil {
		return err
	}
	return p.client().TailEvents(ctx, TailOptions{
		RetryMin: time.Second,
		RetryMax: 30 * time.Second,
		AfterError: func(err error, delay time.Duration, attempt int) {
			_ = p.RecordInteraction(ctx, chatout.AuditInteraction{Kind: "provider.signal.reconnect", Meta: map[string]any{"attempt": attempt, "delay": delay.String(), "err": errString(err)}})
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
	if !p.allowedSource(ev.Envelope) || ev.Envelope.DataMessage == nil {
		return nil
	}
	msg := ev.Envelope.DataMessage
	sender := chatout.Sender{ID: signalSource(ev.Envelope), DisplayName: ev.Envelope.SourceName, ChannelID: p.groupID()}
	if verdict, approvalID, timestamp, ok := p.reactionDecision(msg.Reaction); ok {
		if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
			Kind:      "provider.signal.reaction",
			MessageID: strconv.FormatInt(timestamp, 10),
			Payload:   string(msg.Reaction),
			Sender:    sender,
			Meta:      map[string]any{"approval": approvalID, "verdict": verdict},
		}); err != nil {
			return err
		}
		p.dispatchDecision(approvalID, strconv.FormatInt(timestamp, 10), chatout.Decision{ApprovalID: approvalID, Verdict: verdict, Sender: sender, MessageID: strconv.FormatInt(timestamp, 10)})
		return nil
	}
	text := strings.TrimSpace(msg.Message)
	if text == "" {
		return nil
	}
	if err := p.RecordInteraction(ctx, chatout.AuditInteraction{
		Kind:    "provider.signal.text_in",
		Payload: text,
		Sender:  sender,
		Meta:    map[string]any{"source": sender.ID},
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

func (p *Provider) reactionDecision(raw json.RawMessage) (string, string, int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", 0, false
	}
	var r struct {
		Emoji               string `json:"emoji"`
		TargetSentTimestamp int64  `json:"targetSentTimestamp"`
		TargetTimestamp     int64  `json:"targetTimestamp"`
		Timestamp           int64  `json:"timestamp"`
		Remove              bool   `json:"remove"`
		IsRemove            bool   `json:"isRemove"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Remove || r.IsRemove {
		return "", "", 0, false
	}
	verdict := signalVerdict(r.Emoji)
	if verdict == "" {
		return "", "", 0, false
	}
	ts := r.TargetSentTimestamp
	if ts == 0 {
		ts = r.TargetTimestamp
	}
	if ts == 0 {
		ts = r.Timestamp
	}
	p.mu.Lock()
	id := p.approvals[ts]
	p.mu.Unlock()
	return verdict, id, ts, id != ""
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

func (p *Provider) allowedSource(env Envelope) bool {
	want := strings.TrimSpace(p.Owner)
	if want == "" && p.groupID() == "" && len(p.recipients()) == 1 {
		want = p.recipients()[0]
	}
	if want == "" {
		return true
	}
	for _, got := range []string{env.Source, env.SourceNumber, env.SourceUUID} {
		if strings.EqualFold(strings.TrimSpace(got), want) {
			return true
		}
	}
	return false
}

func (p *Provider) client() *Client {
	if p == nil || p.Client == nil {
		return New("", "")
	}
	return p.Client
}

func (p *Provider) recipients() []string {
	if p == nil {
		return nil
	}
	return cleanRecipients(p.Recipients)
}

func (p *Provider) groupID() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.GroupID)
}

func cleanRecipients(recipients []string) []string {
	out := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		if recipient = strings.TrimSpace(recipient); recipient != "" {
			out = append(out, recipient)
		}
	}
	return out
}

func signalSource(env Envelope) string {
	for _, value := range []string{env.SourceNumber, env.Source, env.SourceUUID, env.SourceName} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown"
}

func signalVerdict(emoji string) string {
	switch strings.TrimSpace(emoji) {
	case "\U0001f44d", "\u2705":
		return "approve"
	case "\U0001f44e", "\u274c":
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
