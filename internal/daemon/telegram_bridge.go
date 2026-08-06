package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/codexapp"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/telegram"
)

const (
	telegramTargetPrefix = "telegram.target."
	telegramPastePrefix  = "telegram.paste."
	telegramCardPrefix   = "telegram.card."
	telegramReplyPrefix  = "telegram.reply."
	telegramCardTTL      = 24 * time.Hour
	telegramPasteTTL     = 5 * time.Minute
)

var codexStatusDelay = time.Second

type telegramCard struct {
	Kind       string              `json:"kind"`
	SessionID  string              `json:"session_id,omitempty"`
	ApprovalID string              `json:"approval_id,omitempty"`
	RequestID  json.RawMessage     `json:"request_id,omitempty"`
	Decision   string              `json:"decision,omitempty"`
	Payload    json.RawMessage     `json:"payload,omitempty"`
	Question   int                 `json:"question,omitempty"`
	Answers    map[string][]string `json:"answers,omitempty"`
	ExpiresAt  int64               `json:"expires_at"`
}
type telegramReply struct {
	SessionID string              `json:"session_id"`
	RequestID json.RawMessage     `json:"request_id"`
	Payload   json.RawMessage     `json:"payload"`
	Question  int                 `json:"question"`
	Answers   map[string][]string `json:"answers"`
	MessageID int64               `json:"message_id"`
}
type codexStatus struct {
	MessageID int64
	Text      string
	Kind      string
	Pending   bool
}
type agentStatus struct {
	MessageID int64
	RunID     string
}
type telegramBridge struct {
	d             *Daemon
	client        *telegram.Client
	mu            sync.Mutex
	ownerID       int64
	ownerUserID   int64
	seen          map[string]bool
	sending       map[string]bool
	killArmed     map[int64]time.Time
	cards         map[string]telegramCard
	statuses      map[string]codexStatus
	agentStatuses map[string]agentStatus
}

func (d *Daemon) runTelegramBridge(ctx context.Context) error {
	c := telegram.NewClient(d.TelegramToken)
	if err := c.DeleteWebhook(ctx); err != nil {
		d.Log.Warn("Telegram delete webhook", "err", err)
	}
	b := &telegramBridge{d: d, client: c, ownerID: d.TelegramOwnerID, ownerUserID: d.TelegramOwnerUserID, seen: map[string]bool{}, sending: map[string]bool{}, killArmed: map[int64]time.Time{}, cards: map[string]telegramCard{}, statuses: map[string]codexStatus{}, agentStatuses: map[string]agentStatus{}}
	go b.forwardApprovals(ctx)
	go b.forwardCodexEvents(ctx)
	go b.forwardAgentEvents(ctx)
	var offset int64
	failures := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		updates, err := c.GetUpdates(ctx, offset, 25)
		if err != nil {
			failures++
			delay := telegram.ReconnectBackoff(failures)
			d.Log.Warn("Telegram poll", "err", err, "backoff", delay)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		failures = 0
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			b.handleUpdate(ctx, update)
		}
	}
}

func (b *telegramBridge) handleUpdate(ctx context.Context, update telegram.Update) {
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
		return
	}
	m := update.Message
	if m == nil || strings.TrimSpace(m.Text) == "" || !b.authorizedOrPair(ctx, m) {
		return
	}
	if b.hasReply(ctx, m.Chat.ID) {
		b.handleReply(ctx, m)
		return
	}
	if strings.HasPrefix(m.Text, "//") {
		literal := *m
		literal.Text = strings.TrimPrefix(m.Text, "/")
		b.handleInput(ctx, &literal)
		return
	}
	if strings.HasPrefix(strings.TrimSpace(m.Text), "/") && b.handleCommand(ctx, m.Chat.ID, m.Text) {
		return
	}
	b.handleInput(ctx, m)
}

func (b *telegramBridge) authorizedOrPair(ctx context.Context, m *telegram.Message) bool {
	if b.isOwner(m) {
		return true
	}
	fields := strings.Fields(strings.TrimSpace(m.Text))
	if m.Chat.Type == "private" && m.From != nil && len(fields) == 2 && strings.EqualFold(fields[0], "/start") && fields[1] == strings.TrimSpace(b.d.TelegramPair) {
		b.setOwner(ctx, m.Chat.ID, m.From.ID)
		b.send(ctx, m.Chat.ID, "Paired. Use /new shell, /new codex, or /new pi.", nil)
		return true
	}
	if b.owner() == 0 && m.Chat.Type == "private" {
		b.send(ctx, m.Chat.ID, "Onibi is not paired. Run onibi telegram setup locally and send its /start code.", nil)
	}
	return false
}

func (b *telegramBridge) handleCommand(ctx context.Context, chatID int64, text string) bool {
	command, arg := splitCommand(text)
	switch command {
	case "/start", "/help":
		b.send(ctx, chatID, telegramHelp(), nil)
	case "/status", "/ping":
		b.send(ctx, chatID, b.d.pingText(ctx)+"\n\n"+b.sessionsText(ctx, chatID), nil)
	case "/sessions":
		b.sendSessions(ctx, chatID)
	case "/target":
		s, err := b.d.sessionByID(strings.TrimSpace(arg))
		if err != nil {
			b.send(ctx, chatID, "Target failed: "+err.Error(), nil)
			return true
		}
		b.setTarget(ctx, chatID, s.ID)
		b.send(ctx, chatID, "Target: "+s.Name+" ("+shortID(s.ID)+").", b.sessionControls(ctx, s.ID))
	case "/new":
		b.handleNew(ctx, chatID, arg)
	case "/tail", "/peek", "/text":
		b.handleTail(ctx, chatID, arg)
	case "/screen":
		b.sendScreen(ctx, chatID, b.target(ctx, chatID), "Screen")
	case "/font":
		b.sendFonts(ctx, chatID)
	case "/paste":
		b.setPaste(ctx, chatID)
		b.send(ctx, chatID, "Paste mode armed. Your next message is sent literally without Enter.", nil)
	case "/keys":
		s, err := b.d.sessionForRPCTarget(b.target(ctx, chatID))
		if err != nil {
			if errors.Is(err, ErrSessionEnded) {
				b.send(ctx, chatID, b.sessionEndedText(ctx, chatID, b.target(ctx, chatID)), nil)
				return true
			}
			b.send(ctx, chatID, "Controls failed: "+err.Error(), nil)
			return true
		}
		b.send(ctx, chatID, "Controls · "+s.Name, b.sessionControls(ctx, s.ID))
	case "/interrupt":
		b.control(ctx, chatID, b.target(ctx, chatID), "interrupt", 0)
	case "/esc":
		b.key(ctx, chatID, b.target(ctx, chatID), "Escape", 0)
	case "/enter":
		b.key(ctx, chatID, b.target(ctx, chatID), "Enter", 0)
	case "/kill":
		b.handleKill(ctx, chatID)
	default:
		return false
	}
	return true
}

func (b *telegramBridge) handleInput(ctx context.Context, m *telegram.Message) {
	s, err := b.d.sessionForRPCTarget(b.target(ctx, m.Chat.ID))
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			b.send(ctx, m.Chat.ID, b.sessionEndedText(ctx, m.Chat.ID, b.target(ctx, m.Chat.ID)), nil)
			return
		}
		b.send(ctx, m.Chat.ID, "Select a session with /sessions or create one with /new shell.", nil)
		return
	}
	paste := b.consumePaste(ctx, m.Chat.ID)
	b.d.audit(ctx, "telegram.input", s.ID, m.Text, m.Chat.ID, fmt.Sprintf("enter=%t", !paste))
	b.d.Log.Info("Telegram input", "session", s.ID, "paste", paste)
	status, err := b.client.SendMessage(ctx, m.Chat.ID, "Running in "+s.Name+"…", nil)
	if err != nil {
		b.d.Log.Warn("Telegram status", "err", err)
		return
	}
	out, err := b.d.SendSessionTextAndCapture(ctx, s.ID, m.Text, !paste)
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			b.edit(ctx, m.Chat.ID, status.MessageID, b.sessionEndedText(ctx, m.Chat.ID, s.ID), nil)
			return
		}
		b.edit(ctx, m.Chat.ID, status.MessageID, "Input failed: "+err.Error(), nil)
		if s.Transport == "tmux" {
			b.sendScreen(ctx, m.Chat.ID, s.ID, "Failed · "+s.Name)
		}
		return
	}
	if s.Transport == "codex" {
		b.setCodexStatusMessage(s.ID, status.MessageID)
		b.edit(ctx, m.Chat.ID, status.MessageID, terminalCard("Codex working · "+s.Name, out), b.sessionControls(ctx, s.ID))
		return
	}
	if (s.Agent == "pi" || s.Agent == "claude") && !paste {
		b.setAgentStatusMessage(s.ID, status.MessageID)
		b.edit(ctx, m.Chat.ID, status.MessageID, terminalCard(claudeAgentTitle(s.Agent)+" working · "+s.Name, out), b.sessionControls(ctx, s.ID))
		return
	}
	label := "Sent · " + s.Name
	if paste {
		label = "Pasted · " + s.Name + "\nUse /enter to submit when ready"
	}
	b.edit(ctx, m.Chat.ID, status.MessageID, terminalCard(label, out), b.sessionControls(ctx, s.ID))
	if !paste && s.Transport == "tmux" {
		b.sendScreen(ctx, m.Chat.ID, s.ID, "Updated · "+s.Name)
	}
}

func (b *telegramBridge) handleNew(ctx context.Context, chatID int64, arg string) {
	agent, name, args, err := parseNewSession(arg)
	if err != nil {
		b.send(ctx, chatID, "Usage: /new [shell|codex|pi] [--name name] [--cwd path] [agent args…]", nil)
		return
	}
	cwd, args, err := extractNewCWD(args)
	if err != nil {
		b.send(ctx, chatID, "New session failed: "+err.Error(), nil)
		return
	}
	if agent == "codex" {
		if len(args) > 0 {
			b.send(ctx, chatID, "Codex accepts only /new codex [--name name] [--cwd path].", nil)
			return
		}
		s, err := b.d.StartCodexSession(ctx, name, cwd)
		if err != nil {
			b.send(ctx, chatID, "Start failed: "+err.Error(), nil)
			return
		}
		b.setTarget(ctx, chatID, s.ID)
		b.send(ctx, chatID, "Codex ready · "+s.Name+"\nSend a normal message to start a turn.", b.sessionControls(ctx, s.ID))
		return
	}
	bin, _, args, ok := b.d.agentCommand(agent, args)
	if !ok {
		b.send(ctx, chatID, "Supported sessions: shell, codex, pi, claude.", nil)
		return
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		b.send(ctx, chatID, bin+" not found in PATH.", nil)
		return
	}
	s, err := b.d.StartTmuxSession(ctx, name, agent, path, args, cwd)
	if err != nil {
		b.send(ctx, chatID, "Start failed: "+err.Error(), nil)
		return
	}
	b.setTarget(ctx, chatID, s.ID)
	b.send(ctx, chatID, "Started "+s.Name+" ("+shortID(s.ID)+").", b.sessionControls(ctx, s.ID))
}

func (b *telegramBridge) handleTail(ctx context.Context, chatID int64, arg string) {
	lines := 80
	if raw := strings.TrimSpace(arg); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 400 {
			b.send(ctx, chatID, "Usage: /tail [1..400]", nil)
			return
		}
		lines = n
	}
	s, err := b.d.sessionForRPCTarget(b.target(ctx, chatID))
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			b.send(ctx, chatID, b.sessionEndedText(ctx, chatID, b.target(ctx, chatID)), nil)
			return
		}
		b.send(ctx, chatID, "Tail failed: "+err.Error(), nil)
		return
	}
	out, err := b.d.CaptureSessionTail(ctx, s.ID, lines)
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			b.send(ctx, chatID, b.sessionEndedText(ctx, chatID, s.ID), nil)
			return
		}
		b.send(ctx, chatID, "Tail failed: "+err.Error(), nil)
		return
	}
	b.send(ctx, chatID, terminalCard("Tail · "+s.Name, out), b.sessionControls(ctx, s.ID))
}
func (b *telegramBridge) handleKill(ctx context.Context, chatID int64) {
	b.mu.Lock()
	armed := b.killArmed[chatID]
	if time.Since(armed) > 2*time.Second {
		b.killArmed[chatID] = time.Now()
		b.mu.Unlock()
		b.send(ctx, chatID, "Send /kill again within 2s to end the selected session.", nil)
		return
	}
	delete(b.killArmed, chatID)
	b.mu.Unlock()
	b.control(ctx, chatID, b.target(ctx, chatID), "kill", 0)
}

func (b *telegramBridge) sendFonts(ctx context.Context, chatID int64) {
	current, _ := b.d.screenFont()
	rows := make([][]telegram.InlineKeyboardButton, 0, 4)
	for _, choice := range b.d.screenFontChoices() {
		token, err := b.newCard(ctx, telegramCard{Kind: "font", Decision: choice.ID})
		if err != nil {
			b.send(ctx, chatID, "Font controls unavailable.", nil)
			return
		}
		label := choice.Label
		if choice.ID == current {
			label = "✓ " + label
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: label, CallbackData: "c:" + token}})
	}
	b.send(ctx, chatID, "Screen font", &telegram.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (b *telegramBridge) setFont(ctx context.Context, chatID, messageID int64, name string) {
	if err := b.d.SetScreenFont(name); err != nil {
		b.edit(ctx, chatID, messageID, "Font change failed: "+err.Error(), nil)
		return
	}
	label := name
	for _, choice := range b.d.screenFontChoices() {
		if choice.ID == name {
			label = choice.Label
			break
		}
	}
	b.edit(ctx, chatID, messageID, "Screen font: "+label, nil)
}

func (b *telegramBridge) sendSessions(ctx context.Context, chatID int64) {
	list := b.d.liveSessions()
	if len(list) == 0 {
		b.send(ctx, chatID, "No active sessions. Use /new shell, /new codex, or /new pi.", nil)
		return
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, len(list))
	for _, s := range list {
		token, err := b.newCard(ctx, telegramCard{Kind: "target", SessionID: s.ID})
		if err != nil {
			b.send(ctx, chatID, "Session controls unavailable.", nil)
			return
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: s.Name + " · " + shortID(s.ID), CallbackData: "c:" + token}})
	}
	b.send(ctx, chatID, b.sessionsText(ctx, chatID), &telegram.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (b *telegramBridge) handleCallback(ctx context.Context, q *telegram.CallbackQuery) {
	if q == nil || q.Message == nil || q.Message.Chat.Type != "private" || q.Message.Chat.ID != b.owner() || q.From.ID != b.ownerUser() {
		if q != nil {
			_ = b.client.AnswerCallbackQuery(ctx, q.ID, "not authorized")
		}
		return
	}
	if !strings.HasPrefix(q.Data, "c:") {
		_ = b.client.AnswerCallbackQuery(ctx, q.ID, "expired control")
		return
	}
	card, ok := b.takeCard(ctx, strings.TrimPrefix(q.Data, "c:"))
	if !ok {
		_ = b.client.AnswerCallbackQuery(ctx, q.ID, "expired control")
		return
	}
	b.d.audit(ctx, "telegram.card", card.SessionID, q.Data, q.Message.Chat.ID, "kind="+card.Kind)
	switch card.Kind {
	case "target":
		s, err := b.d.sessionByID(card.SessionID)
		if err != nil {
			b.edit(ctx, q.Message.Chat.ID, q.Message.MessageID, "Session unavailable.", nil)
		} else {
			b.setTarget(ctx, q.Message.Chat.ID, s.ID)
			b.edit(ctx, q.Message.Chat.ID, q.Message.MessageID, "Target: "+s.Name+" ("+shortID(s.ID)+").", b.sessionControls(ctx, s.ID))
		}
	case "key":
		b.key(ctx, q.Message.Chat.ID, card.SessionID, card.Decision, q.Message.MessageID)
	case "control":
		b.control(ctx, q.Message.Chat.ID, card.SessionID, card.Decision, q.Message.MessageID)
	case "screen":
		b.sendScreen(ctx, q.Message.Chat.ID, card.SessionID, "Screen")
	case "font":
		b.setFont(ctx, q.Message.Chat.ID, q.Message.MessageID, card.Decision)
	case "approval":
		b.resolveApproval(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	case "codex_approval":
		b.resolveCodexApproval(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	case "codex_input":
		b.resolveCodexInput(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	default:
		b.edit(ctx, q.Message.Chat.ID, q.Message.MessageID, "Expired control.", nil)
	}
	_ = b.client.AnswerCallbackQuery(ctx, q.ID, "ok")
}

func (b *telegramBridge) key(ctx context.Context, chatID int64, sessionID, key string, messageID int64) {
	err := b.d.SendSessionKey(ctx, sessionID, key)
	text := key + " sent."
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			text = b.sessionEndedText(ctx, chatID, sessionID)
		} else {
			text = key + " failed: " + err.Error()
		}
	}
	if messageID == 0 {
		b.send(ctx, chatID, text, nil)
	} else {
		b.edit(ctx, chatID, messageID, text, b.sessionControls(ctx, sessionID))
	}
}
func (b *telegramBridge) control(ctx context.Context, chatID int64, sessionID, action string, messageID int64) {
	if action == "" {
		return
	}
	err := b.d.ControlSession(ctx, sessionID, action)
	text := strings.ToUpper(action[:1]) + action[1:] + " sent."
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			text = b.sessionEndedText(ctx, chatID, sessionID)
		} else {
			text = strings.ToUpper(action[:1]) + action[1:] + " failed: " + err.Error()
		}
	} else if action == "kill" {
		text = b.sessionEndedText(ctx, chatID, sessionID)
	}
	if messageID == 0 {
		b.send(ctx, chatID, text, nil)
	} else {
		b.edit(ctx, chatID, messageID, text, nil)
	}
}
func (b *telegramBridge) sessionControls(ctx context.Context, sessionID string) *telegram.InlineKeyboardMarkup {
	s, err := b.d.sessionByID(sessionID)
	if err != nil || s.Ended() {
		return nil
	}
	if s.Transport == "codex" {
		interrupt, e1 := b.newCard(ctx, telegramCard{Kind: "control", SessionID: sessionID, Decision: "interrupt"})
		if e1 != nil {
			return nil
		}
		return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Interrupt", CallbackData: "c:" + interrupt}}}}
	}
	esc, e1 := b.newCard(ctx, telegramCard{Kind: "key", SessionID: sessionID, Decision: "Escape"})
	interrupt, e2 := b.newCard(ctx, telegramCard{Kind: "control", SessionID: sessionID, Decision: "interrupt"})
	enter, e3 := b.newCard(ctx, telegramCard{Kind: "key", SessionID: sessionID, Decision: "Enter"})
	screen, e4 := b.newCard(ctx, telegramCard{Kind: "screen", SessionID: sessionID})
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return nil
	}
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Esc", CallbackData: "c:" + esc}, {Text: "Ctrl-C", CallbackData: "c:" + interrupt}, {Text: "Enter", CallbackData: "c:" + enter}}, {{Text: "Screen", CallbackData: "c:" + screen}}}}
}

func (b *telegramBridge) forwardApprovals(ctx context.Context) {
	events, unsub, err := b.d.Queue.Subscribe()
	if err != nil {
		b.d.Log.Warn("approval subscribe", "err", err)
		return
	}
	defer unsub()
	if b.owner() != 0 {
		b.replayPendingApprovals(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Type == approval.EventRequested {
				item := event.Approval
				b.sendApproval(ctx, &item)
			}
		}
	}
}
func (b *telegramBridge) replayPendingApprovals(ctx context.Context) {
	pending, err := b.d.Queue.Pending(ctx)
	if err != nil {
		b.d.Log.Warn("load pending approvals", "err", err)
		return
	}
	for _, item := range pending {
		b.sendApproval(ctx, item)
	}
}
func (b *telegramBridge) sendApproval(ctx context.Context, item *approval.Approval) {
	if item == nil || b.owner() == 0 {
		return
	}
	b.mu.Lock()
	if b.seen[item.ID] || b.sending[item.ID] {
		b.mu.Unlock()
		return
	}
	b.sending[item.ID] = true
	b.mu.Unlock()
	finish := func(delivered bool) {
		b.mu.Lock()
		delete(b.sending, item.ID)
		if delivered {
			b.seen[item.ID] = true
		}
		b.mu.Unlock()
	}
	model, err := approval.RequestForApproval(*item)
	if err != nil {
		_, _ = b.d.Queue.DecideIdempotently(ctx, item.ID, approval.VerdictDeny, "invalid Pi approval payload", 0)
		b.send(ctx, b.owner(), "Rejected invalid Pi approval payload.", nil)
		finish(true)
		return
	}
	b.sendScreen(ctx, b.owner(), item.SessionID, "Action required")
	approve := "approve"
	if model.Risk.Level == approval.RiskHigh {
		approve = "confirm"
	}
	yes, err := b.newCard(ctx, telegramCard{Kind: "approval", SessionID: item.SessionID, ApprovalID: item.ID, Decision: approve})
	if err != nil {
		finish(false)
		b.retryApproval(ctx, item)
		return
	}
	no, err := b.newCard(ctx, telegramCard{Kind: "approval", SessionID: item.SessionID, ApprovalID: item.ID, Decision: "deny"})
	if err != nil {
		finish(false)
		b.retryApproval(ctx, item)
		return
	}
	sessionName := item.SessionID
	if session, err := b.d.Registry.Get(item.SessionID); err == nil {
		sessionName = session.Name
	}
	_, err = b.send(ctx, b.owner(), formatApproval(item, sessionName), &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Approve", CallbackData: "c:" + yes}, {Text: "Deny", CallbackData: "c:" + no}}}})
	finish(err == nil)
	if err != nil {
		b.retryApproval(ctx, item)
	}
}

func (b *telegramBridge) retryApproval(ctx context.Context, item *approval.Approval) {
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
			b.sendApproval(ctx, item)
		}
	}()
}
func (b *telegramBridge) resolveApproval(ctx context.Context, chatID, messageID int64, card telegramCard) {
	if card.Decision == "confirm" {
		yes, err := b.newCard(ctx, telegramCard{Kind: "approval", SessionID: card.SessionID, ApprovalID: card.ApprovalID, Decision: "approve"})
		if err != nil {
			return
		}
		no, err := b.newCard(ctx, telegramCard{Kind: "approval", SessionID: card.SessionID, ApprovalID: card.ApprovalID, Decision: "deny"})
		if err != nil {
			return
		}
		b.edit(ctx, chatID, messageID, "High-risk approval. Confirm?", &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Confirm approve", CallbackData: "c:" + yes}, {Text: "Deny", CallbackData: "c:" + no}}}})
		return
	}
	verdict := approval.VerdictDeny
	if card.Decision == "approve" {
		verdict = approval.VerdictApprove
	}
	if _, err := b.d.Queue.DecideIdempotently(ctx, card.ApprovalID, verdict, "decided from Telegram", chatID); err != nil {
		b.edit(ctx, chatID, messageID, "Approval failed: "+err.Error(), nil)
		return
	}
	b.edit(ctx, chatID, messageID, "Approval "+string(verdict)+".", nil)
}

func (b *telegramBridge) forwardCodexEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-b.d.CodexEvents():
			if !ok {
				return
			}
			if event.Request != nil {
				b.sendCodexRequest(ctx, event)
				continue
			}
			if event.Text != "" {
				b.updateCodexStatus(ctx, event)
			}
		}
	}
}

func (b *telegramBridge) setCodexStatusMessage(sessionID string, messageID int64) {
	b.mu.Lock()
	state := b.statuses[sessionID]
	if state.MessageID == 0 {
		state.MessageID = messageID
		b.statuses[sessionID] = state
	}
	b.mu.Unlock()
}

func (b *telegramBridge) forwardAgentEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-b.d.AgentEvents():
			if !ok {
				return
			}
			b.updateAgentStatus(ctx, event)
		}
	}
}

func (b *telegramBridge) setAgentStatusMessage(sessionID string, messageID int64) {
	b.mu.Lock()
	state := b.agentStatuses[sessionID]
	if state.MessageID == 0 {
		state.MessageID = messageID
		b.agentStatuses[sessionID] = state
	}
	b.mu.Unlock()
}

func (b *telegramBridge) updateAgentStatus(ctx context.Context, event AgentEvent) {
	if b.owner() == 0 {
		return
	}
	s, err := b.d.Registry.Get(event.SessionID)
	if err != nil {
		return
	}
	b.mu.Lock()
	state := b.agentStatuses[event.SessionID]
	b.mu.Unlock()
	if event.Kind == "agent_start" {
		b.mu.Lock()
		state = b.agentStatuses[event.SessionID]
		state.RunID = event.RunID
		b.agentStatuses[event.SessionID] = state
		b.mu.Unlock()
		if state.MessageID == 0 {
			m, err := b.client.SendMessage(ctx, b.owner(), claudeAgentTitle(event.Agent)+" working · "+s.Name, b.sessionControls(ctx, s.ID))
			if err != nil {
				return
			}
			b.setAgentStatusMessage(s.ID, m.MessageID)
			return
		}
		b.edit(ctx, b.owner(), state.MessageID, claudeAgentTitle(event.Agent)+" working · "+s.Name, b.sessionControls(ctx, s.ID))
		return
	}
	if event.Kind != "agent_end" && event.Kind != "agent_failed" {
		return
	}
	if event.Agent != "claude" && (state.MessageID == 0 || state.RunID != event.RunID) {
		return
	}
	tail, err := b.d.CaptureSessionTail(ctx, s.ID, 80)
	if err != nil {
		tail = "Final output unavailable: " + err.Error()
	}
	result := "completed"
	if event.Kind == "agent_failed" {
		result = "failed"
	}
	title := claudeAgentTitle(event.Agent) + " " + result + " · " + s.Name
	if state.MessageID == 0 {
		b.send(ctx, b.owner(), terminalCard(title, tail), b.sessionControls(ctx, s.ID))
	} else {
		b.edit(ctx, b.owner(), state.MessageID, terminalCard(title, tail), b.sessionControls(ctx, s.ID))
	}
	b.sendScreen(ctx, b.owner(), s.ID, title)
	b.mu.Lock()
	delete(b.agentStatuses, s.ID)
	b.mu.Unlock()
}

func (b *telegramBridge) updateCodexStatus(ctx context.Context, event CodexEvent) {
	if b.owner() == 0 {
		return
	}
	if _, err := b.d.Registry.Get(event.SessionID); err != nil {
		return
	}
	b.mu.Lock()
	state := b.statuses[event.SessionID]
	if event.Kind == "progress" {
		state.Text = appendCodexStatus(state.Text, event.Text)
	}
	if event.Kind == "completed" || event.Kind == "failed" {
		state.Text = appendCodexStatus(state.Text, event.Text)
		state.Kind = event.Kind
	}
	if state.Pending {
		b.statuses[event.SessionID] = state
		b.mu.Unlock()
		return
	}
	state.Pending = true
	b.statuses[event.SessionID] = state
	b.mu.Unlock()
	go b.flushCodexStatus(ctx, event.SessionID)
}

func appendCodexStatus(current, next string) string {
	joined := strings.TrimSpace(current + "\n" + next)
	if len(joined) <= 3600 {
		return joined
	}
	return "…\n" + joined[len(joined)-3598:]
}

func (b *telegramBridge) flushCodexStatus(ctx context.Context, sessionID string) {
	timer := time.NewTimer(codexStatusDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	b.mu.Lock()
	state, ok := b.statuses[sessionID]
	if !ok {
		b.mu.Unlock()
		return
	}
	state.Pending = false
	b.statuses[sessionID] = state
	b.mu.Unlock()
	s, err := b.d.Registry.Get(sessionID)
	if err != nil {
		return
	}
	body := strings.TrimSpace(state.Text)
	if body == "" {
		body = "Codex is working…"
	}
	title := "Codex · " + s.Name
	if state.Kind == "completed" {
		title = "Codex completed · " + s.Name
	}
	if state.Kind == "failed" {
		title = "Codex failed · " + s.Name
	}
	if s.Ended() {
		title = "Codex ended · " + s.Name
		body = strings.TrimSpace(body + "\n\n" + b.sessionEndedText(ctx, b.owner(), s.ID))
	}
	if state.MessageID == 0 {
		m, err := b.client.SendMessage(ctx, b.owner(), terminalCard(title, body), b.sessionControls(ctx, s.ID))
		if err == nil {
			b.mu.Lock()
			latest := b.statuses[sessionID]
			latest.MessageID = m.MessageID
			b.statuses[sessionID] = latest
			b.mu.Unlock()
		}
	} else {
		b.edit(ctx, b.owner(), state.MessageID, terminalCard(title, body), b.sessionControls(ctx, s.ID))
	}
	if (state.Kind == "completed" || state.Kind == "failed") && !s.Ended() {
		b.sendScreen(ctx, b.owner(), s.ID, title)
	}
}

func (b *telegramBridge) sendCodexRequest(ctx context.Context, event CodexEvent) {
	if b.owner() == 0 || event.Request == nil {
		return
	}
	req := event.Request
	var params map[string]any
	if json.Unmarshal(req.Params, &params) != nil {
		b.d.Log.Warn("bad Codex request", "method", req.Method)
		return
	}
	switch req.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		b.sendCodexApproval(ctx, event.SessionID, req, params)
	case "item/tool/requestUserInput":
		b.sendCodexQuestion(ctx, b.owner(), telegramReply{SessionID: event.SessionID, RequestID: req.ID, Payload: req.Params, Answers: map[string][]string{}})
	default:
		b.d.Log.Warn("unsupported Codex server request", "method", req.Method, "session", event.SessionID)
		b.send(ctx, b.owner(), "Codex sent an unsupported decision request: "+req.Method+". Use /interrupt or /kill.", nil)
	}
}
func (b *telegramBridge) sendCodexApproval(ctx context.Context, sessionID string, req *codexapp.ServerRequest, params map[string]any) {
	s, err := b.d.sessionByID(sessionID)
	if err != nil {
		return
	}
	text := formatCodexApproval(req.Method, s, params)
	preview, err := render.RenderPNG([]byte(text), render.PNGOptions{Rows: 16, Cols: 92, Scale: 1})
	if err == nil {
		_ = b.client.SendPhoto(ctx, b.owner(), preview, "Codex decision preview")
	}
	once, err := b.newCard(ctx, telegramCard{Kind: "codex_approval", SessionID: sessionID, RequestID: req.ID, Decision: "accept", Payload: req.Params})
	if err != nil {
		return
	}
	session, err := b.newCard(ctx, telegramCard{Kind: "codex_approval", SessionID: sessionID, RequestID: req.ID, Decision: "acceptForSession", Payload: req.Params})
	if err != nil {
		return
	}
	deny, err := b.newCard(ctx, telegramCard{Kind: "codex_approval", SessionID: sessionID, RequestID: req.ID, Decision: "decline", Payload: req.Params})
	if err != nil {
		return
	}
	cancel, err := b.newCard(ctx, telegramCard{Kind: "codex_approval", SessionID: sessionID, RequestID: req.ID, Decision: "cancel", Payload: req.Params})
	if err != nil {
		return
	}
	b.send(ctx, b.owner(), text, &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Allow once", CallbackData: "c:" + once}, {Text: "Allow session", CallbackData: "c:" + session}}, {{Text: "Deny", CallbackData: "c:" + deny}, {Text: "Cancel", CallbackData: "c:" + cancel}}}})
}
func formatCodexApproval(method string, s *Session, p map[string]any) string {
	var out strings.Builder
	fmt.Fprintln(&out, "Codex decision required")
	fmt.Fprintf(&out, "Session: %s\n", s.Name)
	if reason := stringField(p, "reason"); reason != "" {
		fmt.Fprintf(&out, "Reason: %s\n", approval.Scrub(reason))
	}
	switch method {
	case "item/commandExecution/requestApproval":
		if network, ok := p["networkApprovalContext"].(map[string]any); ok {
			fmt.Fprintln(&out, "Request: network access")
			fmt.Fprintf(&out, "Target: %s %s\n", approval.Scrub(stringField(network, "protocol")), approval.Scrub(stringField(network, "host")))
		} else {
			fmt.Fprintln(&out, "Request: command execution")
			if command := stringField(p, "command"); command != "" {
				fmt.Fprintf(&out, "\n%s\n", approval.Scrub(command))
			}
		}
	case "item/fileChange/requestApproval":
		fmt.Fprintln(&out, "Request: file change")
		if root := stringField(p, "grantRoot"); root != "" {
			fmt.Fprintf(&out, "Root: %s\n", approval.Scrub(root))
		}
	case "item/permissions/requestApproval":
		fmt.Fprintln(&out, "Request: additional permissions")
		if cwd := stringField(p, "cwd"); cwd != "" {
			fmt.Fprintf(&out, "CWD: %s\n", approval.Scrub(cwd))
		}
	}
	return boundedText(strings.TrimSpace(out.String()))
}
func (b *telegramBridge) resolveCodexApproval(ctx context.Context, chatID, messageID int64, card telegramCard) {
	var result any
	if len(card.Payload) == 0 {
		b.edit(ctx, chatID, messageID, "Codex request expired.", nil)
		return
	}
	if isPermissionsRequest(card.Payload) {
		var params map[string]any
		_ = json.Unmarshal(card.Payload, &params)
		permissions := map[string]any{}
		if card.Decision == "accept" || card.Decision == "acceptForSession" {
			if p, ok := params["permissions"]; ok {
				permissions, _ = p.(map[string]any)
			}
		}
		scope := "turn"
		if card.Decision == "acceptForSession" {
			scope = "session"
		}
		result = map[string]any{"permissions": permissions, "scope": scope}
	} else {
		result = map[string]any{"decision": card.Decision}
	}
	if err := b.d.RespondCodexRequest(ctx, card.SessionID, card.RequestID, result); err != nil {
		b.edit(ctx, chatID, messageID, "Codex decision failed: "+err.Error(), nil)
		return
	}
	b.edit(ctx, chatID, messageID, "Codex decision: "+card.Decision+".", nil)
}
func isPermissionsRequest(raw json.RawMessage) bool {
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	_, ok := p["permissions"]
	return ok
}

func (b *telegramBridge) sendCodexQuestion(ctx context.Context, chatID int64, state telegramReply) {
	var params struct {
		Questions []struct {
			Header   string `json:"header"`
			ID       string `json:"id"`
			Question string `json:"question"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
			IsOther bool `json:"isOther"`
		} `json:"questions"`
	}
	if json.Unmarshal(state.Payload, &params) != nil || state.Question >= len(params.Questions) {
		b.finishCodexInput(ctx, chatID, state)
		return
	}
	q := params.Questions[state.Question]
	var out strings.Builder
	fmt.Fprintf(&out, "Codex needs input\n%s\n\n%s", q.Header, q.Question)
	rows := [][]telegram.InlineKeyboardButton{}
	for _, option := range q.Options {
		token, err := b.newCard(ctx, telegramCard{Kind: "codex_input", SessionID: state.SessionID, RequestID: state.RequestID, Decision: option.Label, Payload: state.Payload, Question: state.Question, Answers: state.Answers})
		if err != nil {
			return
		}
		label := option.Label
		if option.Description != "" {
			label += " · " + option.Description
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: boundedButton(label), CallbackData: "c:" + token}})
	}
	if len(q.Options) == 0 || q.IsOther {
		token, err := b.newCard(ctx, telegramCard{Kind: "codex_input", SessionID: state.SessionID, RequestID: state.RequestID, Decision: "other", Payload: state.Payload, Question: state.Question, Answers: state.Answers})
		if err != nil {
			return
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: "Other…", CallbackData: "c:" + token}})
	}
	message, err := b.client.SendMessage(ctx, chatID, boundedText(out.String()), &telegram.InlineKeyboardMarkup{InlineKeyboard: rows})
	if err == nil {
		state.MessageID = message.MessageID
	}
}
func (b *telegramBridge) resolveCodexInput(ctx context.Context, chatID, messageID int64, card telegramCard) {
	state := telegramReply{SessionID: card.SessionID, RequestID: card.RequestID, Payload: card.Payload, Question: card.Question, Answers: card.Answers, MessageID: messageID}
	if card.Decision == "other" {
		b.setReply(ctx, chatID, state)
		b.edit(ctx, chatID, messageID, "Reply with your Codex answer.", nil)
		return
	}
	b.advanceCodexInput(ctx, chatID, state, card.Decision)
}
func (b *telegramBridge) handleReply(ctx context.Context, m *telegram.Message) {
	state, ok := b.reply(ctx, m.Chat.ID)
	if !ok {
		return
	}
	b.clearReply(ctx, m.Chat.ID)
	b.advanceCodexInput(ctx, m.Chat.ID, state, m.Text)
}
func (b *telegramBridge) advanceCodexInput(ctx context.Context, chatID int64, state telegramReply, answer string) {
	var params struct {
		Questions []struct {
			ID string `json:"id"`
		} `json:"questions"`
	}
	if json.Unmarshal(state.Payload, &params) != nil || state.Question >= len(params.Questions) {
		b.send(ctx, chatID, "Codex input expired.", nil)
		return
	}
	if state.Answers == nil {
		state.Answers = map[string][]string{}
	}
	state.Answers[params.Questions[state.Question].ID] = []string{answer}
	state.Question++
	if state.Question < len(params.Questions) {
		b.edit(ctx, chatID, state.MessageID, "Selected: "+answer+".", nil)
		b.sendCodexQuestion(ctx, chatID, state)
		return
	}
	b.finishCodexInput(ctx, chatID, state)
}
func (b *telegramBridge) finishCodexInput(ctx context.Context, chatID int64, state telegramReply) {
	answers := map[string]map[string][]string{}
	for id, values := range state.Answers {
		answers[id] = map[string][]string{"answers": values}
	}
	if err := b.d.RespondCodexRequest(ctx, state.SessionID, state.RequestID, map[string]any{"answers": answers}); err != nil {
		b.send(ctx, chatID, "Codex input failed: "+err.Error(), nil)
		return
	}
	if state.MessageID != 0 {
		b.edit(ctx, chatID, state.MessageID, "Codex input sent.", nil)
	} else {
		b.send(ctx, chatID, "Codex input sent.", nil)
	}
}

func (b *telegramBridge) sendScreen(ctx context.Context, chatID int64, sessionID, caption string) {
	if strings.TrimSpace(sessionID) == "" {
		b.send(ctx, chatID, "No active session. Use /new shell, /new codex, or /new pi.", nil)
		return
	}
	if s, err := b.d.sessionByID(sessionID); err == nil && s.Transport == "codex" && len(s.Buf.Snapshot()) == 0 {
		b.send(ctx, chatID, "Codex has no activity yet. Send a normal message to start a turn.", b.sessionControls(ctx, s.ID))
		return
	}
	png, err := b.d.CaptureSessionScreen(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			b.send(ctx, chatID, b.sessionEndedText(ctx, chatID, sessionID), nil)
			return
		}
		b.send(ctx, chatID, "Screen unavailable: "+err.Error(), nil)
		return
	}
	if err := b.client.SendPhoto(ctx, chatID, png, caption); err != nil {
		b.d.Log.Warn("Telegram screen", "err", err)
		return
	}
	b.d.audit(ctx, "telegram.screen", sessionID, "", chatID, "sent")
}
func (b *telegramBridge) sessionEndedText(ctx context.Context, chatID int64, sessionID string) string {
	name := "Selected session"
	agent := "session"
	if s, err := b.d.Registry.Get(sessionID); err == nil {
		name, agent = s.Name, s.Agent
	}
	if b.target(ctx, chatID) == sessionID {
		b.setTarget(ctx, chatID, "")
	}
	return name + " ended. Screens, input, and controls are unavailable. Use /new " + agent + " to start another."
}
func (b *telegramBridge) newCard(ctx context.Context, card telegramCard) (string, error) {
	now := time.Now()
	if card.ExpiresAt == 0 {
		card.ExpiresAt = now.Add(telegramCardTTL).Unix()
	}
	payload, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	for range 3 {
		var raw [12]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		token := hex.EncodeToString(raw[:])
		b.mu.Lock()
		for key, existing := range b.cards {
			if existing.ExpiresAt > 0 && existing.ExpiresAt <= now.Unix() {
				delete(b.cards, key)
			}
		}
		_, exists := b.cards[token]
		if !exists {
			b.cards[token] = card
		}
		b.mu.Unlock()
		if exists {
			continue
		}
		if b.d.DB != nil {
			if err := b.d.DB.KVSet(ctx, telegramCardPrefix+token, payload, card.ExpiresAt); err != nil {
				b.mu.Lock()
				delete(b.cards, token)
				b.mu.Unlock()
				return "", err
			}
		}
		return token, nil
	}
	return "", fmt.Errorf("generate callback token")
}
func (b *telegramBridge) takeCard(ctx context.Context, token string) (telegramCard, bool) {
	b.mu.Lock()
	card, ok := b.cards[token]
	delete(b.cards, token)
	b.mu.Unlock()
	if !ok && b.d.DB != nil {
		payload, found, err := b.d.DB.KVGet(ctx, telegramCardPrefix+token)
		if err != nil || !found || json.Unmarshal(payload, &card) != nil {
			return telegramCard{}, false
		}
		ok = true
	}
	if b.d.DB != nil {
		_ = b.d.DB.KVDel(ctx, telegramCardPrefix+token)
	}
	if !ok || (card.ExpiresAt > 0 && card.ExpiresAt <= time.Now().Unix()) {
		return telegramCard{}, false
	}
	return card, ok
}
func (b *telegramBridge) owner() int64     { b.mu.Lock(); defer b.mu.Unlock(); return b.ownerID }
func (b *telegramBridge) ownerUser() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.ownerUserID }
func (b *telegramBridge) isOwner(m *telegram.Message) bool {
	return m != nil && m.Chat.Type == "private" && m.From != nil && m.Chat.ID == b.owner() && m.From.ID == b.ownerUser()
}
func (b *telegramBridge) setOwner(ctx context.Context, chat, user int64) {
	b.mu.Lock()
	b.ownerID, b.ownerUserID = chat, user
	b.mu.Unlock()
	b.d.TelegramOwnerID, b.d.TelegramOwnerUserID = chat, user
	if b.d.DB != nil {
		_ = b.d.DB.KVSetString(ctx, TelegramKVOwnerChatID, strconv.FormatInt(chat, 10))
		_ = b.d.DB.KVSetString(ctx, TelegramKVOwnerUserID, strconv.FormatInt(user, 10))
		_ = b.d.DB.KVDel(ctx, TelegramKVPairCode)
	}
	go b.replayPendingApprovals(ctx)
}
func (b *telegramBridge) target(ctx context.Context, chat int64) string {
	if b.d.DB == nil {
		return ""
	}
	v, ok, _ := b.d.DB.KVGetString(ctx, telegramTargetPrefix+strconv.FormatInt(chat, 10))
	if !ok {
		return ""
	}
	return v
}
func (b *telegramBridge) setTarget(ctx context.Context, chat int64, id string) {
	if b.d.DB != nil {
		_ = b.d.DB.KVSetString(ctx, telegramTargetPrefix+strconv.FormatInt(chat, 10), id)
	}
}
func (b *telegramBridge) setPaste(ctx context.Context, chat int64) {
	if b.d.DB != nil {
		_ = b.d.DB.KVSet(ctx, telegramPastePrefix+strconv.FormatInt(chat, 10), []byte("1"), time.Now().Add(telegramPasteTTL).Unix())
	}
}
func (b *telegramBridge) consumePaste(ctx context.Context, chat int64) bool {
	if b.d.DB == nil {
		return false
	}
	key := telegramPastePrefix + strconv.FormatInt(chat, 10)
	_, ok, _ := b.d.DB.KVGetString(ctx, key)
	if ok {
		_ = b.d.DB.KVDel(ctx, key)
	}
	return ok
}
func (b *telegramBridge) setReply(ctx context.Context, chat int64, state telegramReply) {
	if b.d.DB == nil {
		return
	}
	raw, err := json.Marshal(state)
	if err == nil {
		_ = b.d.DB.KVSet(ctx, telegramReplyPrefix+strconv.FormatInt(chat, 10), raw, time.Now().Add(telegramCardTTL).Unix())
	}
}
func (b *telegramBridge) reply(ctx context.Context, chat int64) (telegramReply, bool) {
	if b.d.DB == nil {
		return telegramReply{}, false
	}
	raw, ok, _ := b.d.DB.KVGet(ctx, telegramReplyPrefix+strconv.FormatInt(chat, 10))
	if !ok {
		return telegramReply{}, false
	}
	var state telegramReply
	if json.Unmarshal(raw, &state) != nil {
		return telegramReply{}, false
	}
	return state, true
}
func (b *telegramBridge) hasReply(ctx context.Context, chat int64) bool {
	_, ok := b.reply(ctx, chat)
	return ok
}
func (b *telegramBridge) clearReply(ctx context.Context, chat int64) {
	if b.d.DB != nil {
		_ = b.d.DB.KVDel(ctx, telegramReplyPrefix+strconv.FormatInt(chat, 10))
	}
}
func (b *telegramBridge) sessionsText(ctx context.Context, chat int64) string {
	list := b.d.liveSessions()
	if len(list) == 0 {
		return "No active sessions."
	}
	target := b.target(ctx, chat)
	var out strings.Builder
	out.WriteString("Sessions:\n")
	for _, s := range list {
		mark := " "
		if target == s.ID || (target == "" && len(list) == 1) {
			mark = "*"
		}
		fmt.Fprintf(&out, "%s %s · %s · %s\n", mark, s.Name, shortID(s.ID), s.Agent)
	}
	return strings.TrimSpace(out.String())
}
func (b *telegramBridge) send(ctx context.Context, chat int64, text string, markup *telegram.InlineKeyboardMarkup) (telegram.Message, error) {
	message, err := b.client.SendMessage(ctx, chat, boundedText(text), markup)
	if err != nil {
		b.d.Log.Warn("Telegram send", "chat", chat, "err", err)
	}
	return message, err
}
func (b *telegramBridge) edit(ctx context.Context, chat, message int64, text string, markup *telegram.InlineKeyboardMarkup) {
	if _, err := b.client.EditMessageText(ctx, chat, message, boundedText(text), markup); err != nil {
		b.d.Log.Warn("Telegram edit", "chat", chat, "err", err)
	}
}
func terminalCard(title, body string) string { return title + "\n\n" + strings.TrimSpace(body) }
func boundedText(text string) string {
	if len(text) <= 3800 {
		return text
	}
	return text[:3760] + "\n…\nUse /tail or /screen for more."
}
func boundedButton(text string) string {
	if len(text) <= 56 {
		return text
	}
	return text[:53] + "…"
}
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
func splitCommand(text string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", ""
	}
	command := strings.ToLower(fields[0])
	if i := strings.IndexByte(command, '@'); i >= 0 {
		command = command[:i]
	}
	return command, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), fields[0]))
}
func parseNewSession(arg string) (agent, name string, args []string, err error) {
	fields, err := splitTelegramArgs(arg)
	if err != nil {
		return "", "", nil, err
	}
	if len(fields) == 0 {
		return "shell", "", nil, nil
	}
	agent = strings.ToLower(fields[0])
	for i := 1; i < len(fields); i++ {
		if fields[i] == "--name" {
			if i+1 >= len(fields) || strings.TrimSpace(fields[i+1]) == "" || strings.HasPrefix(fields[i+1], "--") {
				return "", "", nil, fmt.Errorf("name required")
			}
			name = fields[i+1]
			i++
			continue
		}
		args = append(args, fields[i])
	}
	if len(name) > 64 {
		return "", "", nil, fmt.Errorf("name too long")
	}
	return agent, name, args, nil
}

func splitTelegramArgs(input string) ([]string, error) {
	var fields []string
	var current strings.Builder
	quote := byte(0)
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				current.WriteByte(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			flush()
			continue
		}
		current.WriteByte(ch)
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated quoted argument")
	}
	flush()
	return fields, nil
}
func extractNewCWD(args []string) (cwd string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		if args[i] != "--cwd" {
			rest = append(rest, args[i])
			continue
		}
		if cwd != "" || i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			return "", nil, fmt.Errorf("--cwd requires one path")
		}
		cwd = args[i+1]
		i++
	}
	return cwd, rest, nil
}
func telegramHelp() string {
	return "Onibi\n\n/new shell|codex|pi|claude [--name name] [--cwd path]\n/sessions\n/target <id|name>\n/tail [lines]\n/screen\n/font\n/paste\n/keys\n/interrupt\n/esc\n/enter\n/kill\n\nNormal text sends literal input followed by Enter. Unknown /commands go to the selected session; // forces a command through when it conflicts with Onibi. /paste makes exactly the next message literal without Enter."
}
func formatApproval(item *approval.Approval, sessionName string) string {
	if item == nil {
		return "Approval required."
	}
	model, err := approval.PayloadForApproval(*item)
	if err != nil {
		return "Approval required."
	}
	var out strings.Builder
	if sessionName == "" {
		sessionName = model.SessionID
	}
	fmt.Fprintf(&out, "Decision required\nAgent: %s\nTool: %s\nSession: %s\nRisk: %s", model.Agent, model.Tool, sessionName, model.Risk.Level)
	if len(model.Risk.Reasons) > 0 {
		fmt.Fprintf(&out, " (%s)", strings.Join(model.Risk.Reasons, ", "))
	}
	if model.Details.Target != "" {
		fmt.Fprintf(&out, "\n\nTarget:\n%s", model.Details.Target)
	}
	if model.Details.Command != "" {
		fmt.Fprintf(&out, "\n\nCommand:\n%s", model.Details.Command)
	}
	if model.Details.FilePath != "" {
		fmt.Fprintf(&out, "\n\nFile:\n%s", model.Details.FilePath)
	}
	return boundedText(out.String())
}
func stringField(value map[string]any, key string) string { v, _ := value[key].(string); return v }
