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
	"github.com/gongahkia/onibi/internal/store"
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
var screenAutoDelay = 500 * time.Millisecond

type telegramCard struct {
	Kind       string              `json:"kind"`
	SessionID  string              `json:"session_id,omitempty"`
	ApprovalID string              `json:"approval_id,omitempty"`
	RequestID  json.RawMessage     `json:"request_id,omitempty"`
	QuestionID string              `json:"question_id,omitempty"`
	Decision   string              `json:"decision,omitempty"`
	Payload    json.RawMessage     `json:"payload,omitempty"`
	Question   int                 `json:"question,omitempty"`
	Answers    map[string][]string `json:"answers,omitempty"`
	ExpiresAt  int64               `json:"expires_at"`
}
type telegramReply struct {
	Kind       string              `json:"kind,omitempty"`
	SessionID  string              `json:"session_id"`
	QuestionID string              `json:"question_id,omitempty"`
	RequestID  json.RawMessage     `json:"request_id"`
	Payload    json.RawMessage     `json:"payload"`
	Question   int                 `json:"question"`
	Answers    map[string][]string `json:"answers"`
	MessageID  int64               `json:"message_id"`
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
type scheduledScreen struct {
	ChatID    int64
	SessionID string
	Title     string
	Statuses  []int64
	Timer     *time.Timer
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
	autoScreens   map[string]*scheduledScreen
	lastScreens   map[string]string
	outboxWake    chan struct{}
}

func (d *Daemon) runTelegramBridge(ctx context.Context) error {
	c := telegram.NewClient(d.TelegramToken)
	if err := c.DeleteWebhook(ctx); err != nil {
		d.Log.Warn("Telegram delete webhook", "err", err)
	}
	b := &telegramBridge{d: d, client: c, ownerID: d.TelegramOwnerID, ownerUserID: d.TelegramOwnerUserID, seen: map[string]bool{}, sending: map[string]bool{}, killArmed: map[int64]time.Time{}, cards: map[string]telegramCard{}, statuses: map[string]codexStatus{}, agentStatuses: map[string]agentStatus{}, autoScreens: map[string]*scheduledScreen{}, lastScreens: map[string]string{}, outboxWake: make(chan struct{}, 1)}
	if err := d.DB.TelegramOutboxRecover(ctx); err != nil {
		d.Log.Warn("recover Telegram outbox", "err", err)
	}
	if n, err := d.DB.TelegramMarkUncertainUpdates(ctx); err != nil {
		d.Log.Warn("recover Telegram updates", "err", err)
	} else if n > 0 && b.owner() != 0 {
		b.enqueueOutbox(ctx, "notice", b.owner(), "", "Onibi restarted before "+strconv.FormatInt(n, 10)+" inbound update(s) completed. No command was replayed; inspect the session and resend if needed.", 0, "inbound-recovery")
	}
	go b.forwardApprovals(ctx)
	go b.forwardCodexEvents(ctx)
	go b.forwardAgentEvents(ctx)
	go b.forwardClaudeQuestions(ctx)
	go b.forwardSessionEvents(ctx)
	go b.runOutbox(ctx)
	offset, err := d.DB.TelegramNextOffset(ctx)
	if err != nil {
		return err
	}
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
			claimed, err := d.DB.TelegramClaimUpdate(ctx, update.UpdateID)
			if err != nil {
				d.Log.Warn("claim Telegram update", "update", update.UpdateID, "err", err)
				break
			}
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if !claimed {
				continue
			}
			b.handleUpdate(ctx, update)
			if err := d.DB.TelegramCompleteUpdate(ctx, update.UpdateID); err != nil {
				d.Log.Warn("complete Telegram update", "update", update.UpdateID, "err", err)
			}
		}
	}
}

func (b *telegramBridge) handleUpdate(ctx context.Context, update telegram.Update) {
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
		return
	}
	m := update.Message
	if m == nil || (strings.TrimSpace(m.Text) == "" && m.Document == nil) || !b.authorizedOrPair(ctx, m) {
		return
	}
	if m.Document != nil {
		b.handleDocument(ctx, m)
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
		b.send(ctx, m.Chat.ID, "Paired. Use /new shell, /new codex, /new pi, or /new claude.", nil)
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
			if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, b.target(ctx, chatID)) {
				b.send(ctx, chatID, b.sessionEndedText(ctx, chatID, b.target(ctx, chatID)), nil)
				return true
			}
			b.send(ctx, chatID, "Controls failed: "+err.Error(), nil)
			return true
		}
		b.send(ctx, chatID, "Controls · "+s.Name, b.sessionControls(ctx, s.ID))
	case "/key":
		key, err := terminalKey(arg)
		if err != nil {
			b.send(ctx, chatID, "Key failed: "+err.Error(), nil)
			return true
		}
		b.key(ctx, chatID, b.target(ctx, chatID), key, 0)
	case "/size":
		b.resize(ctx, chatID, b.target(ctx, chatID), strings.TrimSpace(arg), 0)
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
		if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, b.target(ctx, m.Chat.ID)) {
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
	}
	var out string
	if s.Transport == "codex" {
		out, err = b.d.SendSessionTextAndCapture(ctx, s.ID, m.Text, !paste)
	} else {
		err = b.d.SendSessionText(ctx, s.ID, m.Text, !paste)
	}
	if err != nil {
		if errors.Is(err, ErrSessionEnded) {
			b.enqueueOutbox(ctx, "ended", m.Chat.ID, s.ID, "", 0, "ended:"+s.ID)
			if status.MessageID != 0 {
				_ = b.edit(ctx, m.Chat.ID, status.MessageID, "Session ended.", nil)
			}
			return
		}
		if status.MessageID == 0 {
			b.enqueueTail(ctx, m.Chat.ID, s.ID, "Input failed · "+s.Name)
		} else if b.edit(ctx, m.Chat.ID, status.MessageID, "Input failed: "+err.Error(), nil) != nil {
			b.enqueueOutbox(ctx, "notice", m.Chat.ID, "", "Input failed in "+s.Name+". Inspect the session with /tail or /screen.", 0, "input-failed:"+strconv.FormatInt(m.Chat.ID, 10)+":"+s.ID)
		}
		if s.Transport == "tmux" {
			b.sendScreen(ctx, m.Chat.ID, s.ID, "Failed · "+s.Name)
		}
		return
	}
	if s.Transport == "codex" {
		if status.MessageID == 0 {
			b.enqueueTail(ctx, m.Chat.ID, s.ID, "Codex working · "+s.Name)
		} else {
			b.setCodexStatusMessage(s.ID, status.MessageID)
			if b.edit(ctx, m.Chat.ID, status.MessageID, terminalCard("Codex working · "+s.Name, out), b.sessionControls(ctx, s.ID)) != nil {
				b.enqueueTail(ctx, m.Chat.ID, s.ID, "Codex working · "+s.Name)
			}
		}
		return
	}
	if (s.Agent == "pi" || s.Agent == "claude") && !paste {
		if status.MessageID == 0 {
			b.enqueueTail(ctx, m.Chat.ID, s.ID, claudeAgentTitle(s.Agent)+" working · "+s.Name)
		} else {
			b.setAgentStatusMessage(s.ID, status.MessageID)
			if b.edit(ctx, m.Chat.ID, status.MessageID, claudeAgentTitle(s.Agent)+" working · "+s.Name, b.sessionControls(ctx, s.ID)) != nil {
				b.enqueueTail(ctx, m.Chat.ID, s.ID, claudeAgentTitle(s.Agent)+" working · "+s.Name)
			}
		}
		return
	}
	label := "Sent · " + s.Name
	if paste {
		label = "Pasted · " + s.Name + "\nUse /enter to submit when ready"
	}
	if paste {
		if status.MessageID == 0 {
			b.enqueueTail(ctx, m.Chat.ID, s.ID, label)
		} else if b.edit(ctx, m.Chat.ID, status.MessageID, label, b.sessionControls(ctx, s.ID)) != nil {
			b.enqueueTail(ctx, m.Chat.ID, s.ID, label)
		}
		return
	}
	b.scheduleAutoScreen(ctx, m.Chat.ID, s.ID, label, status.MessageID)
}

func (b *telegramBridge) handleDocument(ctx context.Context, m *telegram.Message) {
	if m.Document == nil {
		return
	}
	s, err := b.d.sessionForRPCTarget(b.target(ctx, m.Chat.ID))
	if err != nil {
		if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, b.target(ctx, m.Chat.ID)) {
			b.send(ctx, m.Chat.ID, b.sessionEndedText(ctx, m.Chat.ID, b.target(ctx, m.Chat.ID)), nil)
			return
		}
		b.send(ctx, m.Chat.ID, "Select a session before uploading a file.", nil)
		return
	}
	if m.Document.FileSize > b.d.UploadMaxBytes {
		b.send(ctx, m.Chat.ID, fmt.Sprintf("Upload rejected: limit is %d bytes.", b.d.UploadMaxBytes), nil)
		return
	}
	file, err := b.client.GetFile(ctx, m.Document.FileID)
	if err != nil {
		b.send(ctx, m.Chat.ID, "Upload failed: "+err.Error(), nil)
		return
	}
	if file.FileSize > b.d.UploadMaxBytes {
		b.send(ctx, m.Chat.ID, fmt.Sprintf("Upload rejected: limit is %d bytes.", b.d.UploadMaxBytes), nil)
		return
	}
	body, length, err := b.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		b.send(ctx, m.Chat.ID, "Upload failed: "+err.Error(), nil)
		return
	}
	defer body.Close()
	if length > b.d.UploadMaxBytes {
		b.send(ctx, m.Chat.ID, fmt.Sprintf("Upload rejected: limit is %d bytes.", b.d.UploadMaxBytes), nil)
		return
	}
	path, expires, err := b.d.StageUpload(ctx, s.ID, m.Document.FileName, m.Document.FileSize, body)
	if err != nil {
		b.send(ctx, m.Chat.ID, "Upload failed: "+err.Error(), nil)
		return
	}
	b.d.audit(ctx, "telegram.upload", s.ID, "", m.Chat.ID, "path="+path)
	b.send(ctx, m.Chat.ID, "Staged for "+s.Name+"\n"+path+"\nExpires: "+expires.Format(time.RFC3339)+"\nNot inserted or executed.", b.sessionControls(ctx, s.ID))
}

func (b *telegramBridge) handleNew(ctx context.Context, chatID int64, arg string) {
	agent, name, args, err := parseNewSession(arg)
	if err != nil {
		b.send(ctx, chatID, "Usage: /new [shell|codex|pi|claude] [--name name] [--cwd path] [agent args…]", nil)
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
		if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, b.target(ctx, chatID)) {
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
	case "resize":
		b.resize(ctx, q.Message.Chat.ID, card.SessionID, card.Decision, q.Message.MessageID)
	case "font":
		b.setFont(ctx, q.Message.Chat.ID, q.Message.MessageID, card.Decision)
	case "approval":
		b.resolveApproval(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	case "codex_approval":
		b.resolveCodexApproval(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	case "codex_input":
		b.resolveCodexInput(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	case "claude_question":
		b.resolveClaudeQuestion(ctx, q.Message.Chat.ID, q.Message.MessageID, card)
	default:
		b.edit(ctx, q.Message.Chat.ID, q.Message.MessageID, "Expired control.", nil)
	}
	_ = b.client.AnswerCallbackQuery(ctx, q.ID, "ok")
}

func (b *telegramBridge) key(ctx context.Context, chatID int64, sessionID, key string, messageID int64) {
	err := b.d.SendSessionKey(ctx, sessionID, key)
	text := key + " sent."
	if err != nil {
		if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, sessionID) {
			b.enqueueOutbox(ctx, "ended", chatID, sessionID, "", 0, "ended:"+sessionID)
			text = "Session ended."
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
func (b *telegramBridge) resize(ctx context.Context, chatID int64, sessionID, size string, messageID int64) {
	cols, rows, err := b.d.ResizeSession(ctx, sessionID, size)
	text := "Viewport: " + strconv.Itoa(cols) + "×" + strconv.Itoa(rows) + "."
	if err != nil {
		if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, sessionID) {
			text = b.sessionEndedText(ctx, chatID, sessionID)
		} else {
			text = "Resize failed: " + err.Error()
		}
	}
	if messageID == 0 {
		b.send(ctx, chatID, text, b.sessionControls(ctx, sessionID))
	} else {
		b.edit(ctx, chatID, messageID, text, b.sessionControls(ctx, sessionID))
	}
	if err == nil {
		b.sendScreen(ctx, chatID, sessionID, "Viewport · "+size)
	}
}
func (b *telegramBridge) control(ctx context.Context, chatID int64, sessionID, action string, messageID int64) {
	if action == "" {
		return
	}
	err := b.d.ControlSession(ctx, sessionID, action)
	text := strings.ToUpper(action[:1]) + action[1:] + " sent."
	if err != nil {
		if errors.Is(err, ErrSessionEnded) || b.isSessionEnded(ctx, sessionID) {
			b.enqueueOutbox(ctx, "ended", chatID, sessionID, "", 0, "ended:"+sessionID)
			text = "Session ended."
		} else {
			text = strings.ToUpper(action[:1]) + action[1:] + " failed: " + err.Error()
		}
	} else if action == "kill" {
		b.enqueueOutbox(ctx, "ended", chatID, sessionID, "", 0, "ended:"+sessionID)
		text = "Session ended."
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
	button := func(text, kind, decision string) (telegram.InlineKeyboardButton, error) {
		token, err := b.newCard(ctx, telegramCard{Kind: kind, SessionID: sessionID, Decision: decision})
		if err != nil {
			return telegram.InlineKeyboardButton{}, err
		}
		return telegram.InlineKeyboardButton{Text: text, CallbackData: "c:" + token}, nil
	}
	row := func(values ...[3]string) ([]telegram.InlineKeyboardButton, error) {
		out := make([]telegram.InlineKeyboardButton, 0, len(values))
		for _, value := range values {
			item, err := button(value[0], value[1], value[2])
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, 6)
	for _, values := range [][][3]string{
		{{"↑", "key", "Up"}, {"↓", "key", "Down"}, {"←", "key", "Left"}, {"→", "key", "Right"}},
		{{"Tab", "key", "Tab"}, {"⇧Tab", "key", "BTab"}, {"⌫", "key", "BSpace"}, {"Del", "key", "DC"}},
		{{"Home", "key", "Home"}, {"End", "key", "End"}, {"PgUp", "key", "PPage"}, {"PgDn", "key", "NPage"}},
		{{"Esc", "key", "Escape"}, {"Ctrl-C", "control", "interrupt"}, {"Ctrl-D", "key", "C-d"}, {"Ctrl-Z", "key", "C-z"}},
		{{"Ctrl-L", "key", "C-l"}, {"Ctrl-R", "key", "C-r"}, {"Enter", "key", "Enter"}, {"Screen", "screen", ""}},
		{{"80×24", "resize", "small"}, {"100×30", "resize", "medium"}, {"120×40", "resize", "large"}},
	} {
		items, err := row(values...)
		if err != nil {
			return nil
		}
		rows = append(rows, items)
	}
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
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

func (b *telegramBridge) forwardClaudeQuestions(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-b.d.ClaudeQuestionEvents():
			if !ok {
				return
			}
			if event.Question != nil && b.owner() != 0 {
				b.sendClaudeQuestion(ctx, b.owner(), event.Question, 0, map[string][]string{}, 0)
			}
		}
	}
}

func (b *telegramBridge) forwardSessionEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-b.d.SessionEvents():
			if !ok {
				return
			}
			b.d.Log.Debug("session event", "session", event.SessionID, "reason", event.Reason)
			b.wakeOutbox()
		}
	}
}

func (b *telegramBridge) wakeOutbox() {
	if b.outboxWake == nil {
		return
	}
	select {
	case b.outboxWake <- struct{}{}:
	default:
	}
}

func (b *telegramBridge) enqueueOutbox(ctx context.Context, kind string, chatID int64, sessionID, title string, lines int, dedupe string) {
	if b.d.DB == nil || chatID == 0 {
		return
	}
	if err := b.d.DB.TelegramOutboxUpsert(ctx, store.TelegramOutboxIntent{ID: NewID(), DedupeKey: dedupe, Kind: kind, ChatID: chatID, SessionID: sessionID, Title: title, Lines: lines}); err != nil {
		b.d.Log.Warn("queue Telegram outbox", "kind", kind, "err", err)
		return
	}
	b.wakeOutbox()
	b.deliverOutbox(ctx)
}

func (b *telegramBridge) queueScreen(ctx context.Context, chatID int64, sessionID, title string, force bool, at time.Time, deliver bool) {
	if b.d.DB == nil || chatID == 0 {
		return
	}
	item := store.TelegramOutboxIntent{ID: NewID(), DedupeKey: "screen:" + strconv.FormatInt(chatID, 10) + ":" + sessionID, Kind: "screen", ChatID: chatID, SessionID: sessionID, Title: title, ForceScreen: force, NextAttempt: at}
	if err := b.d.DB.TelegramOutboxUpsert(ctx, item); err != nil {
		b.d.Log.Warn("queue Telegram screen", "err", err)
		return
	}
	if deliver {
		b.wakeOutbox()
		b.deliverOutbox(ctx)
	}
}

func (b *telegramBridge) scheduleAutoScreen(ctx context.Context, chatID int64, sessionID, title string, statusID int64) {
	key := strconv.FormatInt(chatID, 10) + ":" + sessionID
	at := time.Now().Add(screenAutoDelay)
	b.queueScreen(ctx, chatID, sessionID, "Updated · "+title, false, at, false)
	b.mu.Lock()
	if b.autoScreens == nil {
		b.autoScreens = map[string]*scheduledScreen{}
	}
	state := b.autoScreens[key]
	if state == nil {
		state = &scheduledScreen{ChatID: chatID, SessionID: sessionID}
		b.autoScreens[key] = state
	}
	if state.Timer != nil {
		state.Timer.Stop()
	}
	state.Title = title
	if statusID != 0 {
		state.Statuses = append(state.Statuses, statusID)
	}
	state.Timer = time.AfterFunc(screenAutoDelay, func() { b.flushAutoScreen(ctx, key) })
	b.mu.Unlock()
}

func (b *telegramBridge) flushAutoScreen(ctx context.Context, key string) {
	if ctx.Err() != nil {
		return
	}
	b.mu.Lock()
	state := b.autoScreens[key]
	delete(b.autoScreens, key)
	b.mu.Unlock()
	if state == nil {
		return
	}
	frame, err := b.d.CaptureSessionFrame(ctx, state.SessionID)
	if err != nil {
		for _, statusID := range state.Statuses {
			_ = b.edit(ctx, state.ChatID, statusID, "Output unavailable: "+err.Error(), nil)
		}
		b.wakeOutbox()
		return
	}
	for _, statusID := range state.Statuses {
		if b.edit(ctx, state.ChatID, statusID, terminalCard(state.Title, frame.Tail), b.sessionControls(ctx, state.SessionID)) != nil {
			b.enqueueTail(ctx, state.ChatID, state.SessionID, state.Title)
		}
	}
	b.wakeOutbox()
	b.deliverOutbox(ctx)
}

func (b *telegramBridge) enqueueTail(ctx context.Context, chatID int64, sessionID, title string) {
	b.enqueueOutbox(ctx, "tail", chatID, sessionID, title, 80, "tail:"+strconv.FormatInt(chatID, 10)+":"+sessionID)
}

func (b *telegramBridge) runOutbox(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-b.outboxWake:
		}
		b.deliverOutbox(ctx)
	}
}

func (b *telegramBridge) deliverOutbox(ctx context.Context) {
	for range 8 {
		item, err := b.d.DB.TelegramOutboxClaim(ctx)
		if err != nil {
			b.d.Log.Warn("claim Telegram outbox", "err", err)
			return
		}
		if item == nil {
			return
		}
		if err := b.dispatchOutbox(ctx, *item); err != nil {
			if outboxPermanent(err) {
				_ = b.d.DB.TelegramOutboxDelivered(ctx, item.ID)
			} else {
				next := time.Now().Add(outboxDelay(item.Attempts + 1))
				_ = b.d.DB.TelegramOutboxRetry(ctx, item.ID, err.Error(), next)
			}
			continue
		}
		_ = b.d.DB.TelegramOutboxDelivered(ctx, item.ID)
	}
}

func (b *telegramBridge) dispatchOutbox(ctx context.Context, item store.TelegramOutboxIntent) error {
	switch item.Kind {
	case "notice":
		_, err := b.client.SendMessage(ctx, item.ChatID, boundedText(item.Title), nil)
		return err
	case "ended":
		return b.sendOutboxEnded(ctx, item)
	case "tail":
		return b.sendOutboxTail(ctx, item)
	case "screen":
		frame, ok := b.d.CachedSessionFrame(item.SessionID)
		if !ok {
			var err error
			frame, err = b.d.CaptureSessionFrame(ctx, item.SessionID)
			if errors.Is(err, ErrSessionEnded) {
				b.enqueueOutbox(ctx, "ended", item.ChatID, item.SessionID, "", 0, "ended:"+item.SessionID)
				return nil
			}
			if err != nil {
				return err
			}
		}
		if !item.ForceScreen && b.screenAlreadySent(item.ChatID, item.SessionID, frame.Fingerprint) {
			return nil
		}
		if err := b.client.SendPhoto(ctx, item.ChatID, frame.PNG, item.Title); err != nil {
			return err
		}
		b.rememberScreen(item.ChatID, item.SessionID, frame.Fingerprint)
		b.d.audit(ctx, "telegram.screen", item.SessionID, "", item.ChatID, "sent")
		return nil
	default:
		return errors.New("unknown Telegram outbox intent")
	}
}

func (b *telegramBridge) screenAlreadySent(chatID int64, sessionID, fingerprint string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastScreens != nil && b.lastScreens[strconv.FormatInt(chatID, 10)+":"+sessionID] == fingerprint
}
func (b *telegramBridge) rememberScreen(chatID int64, sessionID, fingerprint string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastScreens == nil {
		b.lastScreens = map[string]string{}
	}
	b.lastScreens[strconv.FormatInt(chatID, 10)+":"+sessionID] = fingerprint
}

func (b *telegramBridge) sendOutboxTail(ctx context.Context, item store.TelegramOutboxIntent) error {
	out, err := b.d.CaptureSessionTail(ctx, item.SessionID, item.Lines)
	if errors.Is(err, ErrSessionEnded) {
		b.enqueueOutbox(ctx, "ended", item.ChatID, item.SessionID, "", 0, "ended:"+item.SessionID)
		return nil
	}
	if err != nil {
		return err
	}
	_, err = b.client.SendMessage(ctx, item.ChatID, terminalCard(item.Title, out), b.sessionControls(ctx, item.SessionID))
	return err
}

func (b *telegramBridge) sendOutboxEnded(ctx context.Context, item store.TelegramOutboxIntent) error {
	text := b.sessionEndedText(ctx, item.ChatID, item.SessionID)
	if strings.TrimSpace(item.Title) != "" {
		text = item.Title
	}
	_, err := b.client.SendMessage(ctx, item.ChatID, text, nil)
	return err
}

func outboxDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << minInt(attempt-1, 8)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func outboxPermanent(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "chat not found") || strings.Contains(text, "bot was blocked") || strings.Contains(text, "forbidden")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	frame, err := b.d.CaptureSessionFrame(ctx, s.ID)
	tail := frame.Tail
	if err != nil {
		tail = "Final output unavailable: " + err.Error()
	}
	result := "completed"
	if event.Kind == "agent_failed" {
		result = "failed"
	}
	title := claudeAgentTitle(event.Agent) + " " + result + " · " + s.Name
	if state.MessageID == 0 {
		if _, err := b.send(ctx, b.owner(), terminalCard(title, tail), b.sessionControls(ctx, s.ID)); err != nil {
			b.enqueueTail(ctx, b.owner(), s.ID, title)
		}
	} else if b.edit(ctx, b.owner(), state.MessageID, terminalCard(title, tail), b.sessionControls(ctx, s.ID)) != nil {
		b.enqueueTail(ctx, b.owner(), s.ID, title)
	}
	if err == nil {
		b.queueScreen(ctx, b.owner(), s.ID, title, true, time.Now(), true)
	}
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
		} else if state.Kind == "completed" || state.Kind == "failed" {
			b.enqueueTail(ctx, b.owner(), s.ID, title)
		}
	} else if b.edit(ctx, b.owner(), state.MessageID, terminalCard(title, body), b.sessionControls(ctx, s.ID)) != nil && (state.Kind == "completed" || state.Kind == "failed") {
		b.enqueueTail(ctx, b.owner(), s.ID, title)
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
	if state.Kind == "claude_question" {
		b.advanceClaudeQuestion(ctx, m.Chat.ID, state.QuestionID, state.Question, state.Answers, strings.TrimSpace(m.Text), state.MessageID)
		return
	}
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

func (b *telegramBridge) sendClaudeQuestion(ctx context.Context, chatID int64, item *store.ClaudeQuestion, question int, answers map[string][]string, messageID int64) {
	if item == nil || item.State != store.ClaudeQuestionPending || !time.Now().Before(item.ExpiresAt) {
		return
	}
	input, _, err := parseClaudeQuestionInput(item.InputJSON)
	if err != nil || question < 0 || question >= len(input.Questions) {
		return
	}
	q := input.Questions[question]
	var out strings.Builder
	fmt.Fprintf(&out, "Claude needs input (%d/%d)\n%s\n\n%s", question+1, len(input.Questions), q.Header, q.Question)
	selected := map[string]bool{}
	for _, value := range answers[q.Question] {
		selected[value] = true
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, len(q.Options)+2)
	card := func(decision, choice string) (string, error) {
		payload, err := json.Marshal(choice)
		if err != nil {
			return "", err
		}
		return b.newCard(ctx, telegramCard{Kind: "claude_question", SessionID: item.SessionID, QuestionID: item.ID, Decision: decision, Payload: payload, Question: question, Answers: answers, ExpiresAt: item.ExpiresAt.Unix()})
	}
	for _, option := range q.Options {
		token, err := card("select", option.Label)
		if err != nil {
			return
		}
		label := option.Label
		if selected[option.Label] {
			label = "✓ " + label
		}
		if option.Description != "" {
			label += " · " + option.Description
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: boundedButton(label), CallbackData: "c:" + token}})
	}
	other, err := card("other", "")
	if err != nil {
		return
	}
	cancel, err := card("cancel", "")
	if err != nil {
		return
	}
	last := []telegram.InlineKeyboardButton{{Text: "Other…", CallbackData: "c:" + other}}
	if q.MultiSelect {
		continueToken, err := card("continue", "")
		if err != nil {
			return
		}
		last = append(last, telegram.InlineKeyboardButton{Text: "Continue", CallbackData: "c:" + continueToken})
	}
	last = append(last, telegram.InlineKeyboardButton{Text: "Cancel", CallbackData: "c:" + cancel})
	rows = append(rows, last)
	markup := &telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
	if messageID == 0 {
		_, _ = b.client.SendMessage(ctx, chatID, boundedText(out.String()), markup)
		return
	}
	_ = b.edit(ctx, chatID, messageID, boundedText(out.String()), markup)
}

func (b *telegramBridge) resolveClaudeQuestion(ctx context.Context, chatID, messageID int64, card telegramCard) {
	item, found, err := b.d.ClaudeQuestion(ctx, card.QuestionID)
	if err != nil || !found || item.State != store.ClaudeQuestionPending || !time.Now().Before(item.ExpiresAt) {
		b.edit(ctx, chatID, messageID, "Claude question expired.", nil)
		return
	}
	if card.Decision == "cancel" {
		if err := b.d.CancelClaudeQuestion(ctx, item.ID, "cancelled from Telegram", chatID); err != nil {
			b.edit(ctx, chatID, messageID, "Claude question expired.", nil)
			return
		}
		b.edit(ctx, chatID, messageID, "Claude question cancelled.", nil)
		return
	}
	input, _, err := parseClaudeQuestionInput(item.InputJSON)
	if err != nil || card.Question < 0 || card.Question >= len(input.Questions) {
		b.edit(ctx, chatID, messageID, "Claude question expired.", nil)
		return
	}
	q := input.Questions[card.Question]
	if card.Decision == "other" {
		b.setReply(ctx, chatID, telegramReply{Kind: "claude_question", SessionID: item.SessionID, QuestionID: item.ID, Question: card.Question, Answers: card.Answers, MessageID: messageID})
		b.edit(ctx, chatID, messageID, "Reply with your Claude answer.", nil)
		return
	}
	answers := cloneAnswers(card.Answers)
	if card.Decision == "select" {
		var choice string
		if json.Unmarshal(card.Payload, &choice) != nil {
			b.edit(ctx, chatID, messageID, "Claude option expired.", nil)
			return
		}
		choice = strings.TrimSpace(choice)
		if !claudeOptionExists(q, choice) {
			b.edit(ctx, chatID, messageID, "Claude option expired.", nil)
			return
		}
		if q.MultiSelect {
			answers[q.Question] = toggleAnswer(answers[q.Question], choice)
			b.sendClaudeQuestion(ctx, chatID, item, card.Question, answers, messageID)
			return
		}
		b.advanceClaudeQuestion(ctx, chatID, item.ID, card.Question, answers, choice, messageID)
		return
	}
	if card.Decision == "continue" {
		if len(answers[q.Question]) == 0 {
			b.edit(ctx, chatID, messageID, "Select at least one option.", nil)
			return
		}
		b.advanceClaudeQuestion(ctx, chatID, item.ID, card.Question, answers, "", messageID)
	}
}

func (b *telegramBridge) advanceClaudeQuestion(ctx context.Context, chatID int64, id string, question int, answers map[string][]string, answer string, messageID int64) {
	item, found, err := b.d.ClaudeQuestion(ctx, id)
	if err != nil || !found || item.State != store.ClaudeQuestionPending {
		b.edit(ctx, chatID, messageID, "Claude question expired.", nil)
		return
	}
	input, _, err := parseClaudeQuestionInput(item.InputJSON)
	if err != nil || question < 0 || question >= len(input.Questions) {
		b.edit(ctx, chatID, messageID, "Claude question expired.", nil)
		return
	}
	if answers == nil {
		answers = map[string][]string{}
	}
	q := input.Questions[question]
	answer = strings.TrimSpace(answer)
	if answer != "" {
		if q.MultiSelect && len(answers[q.Question]) > 0 {
			answers[q.Question] = append(answers[q.Question], answer)
		} else {
			answers[q.Question] = []string{answer}
		}
	}
	question++
	if question < len(input.Questions) {
		b.edit(ctx, chatID, messageID, "Selected.", nil)
		b.sendClaudeQuestion(ctx, chatID, item, question, answers, 0)
		return
	}
	final := make(map[string]string, len(answers))
	for key, values := range answers {
		final[key] = strings.Join(values, ",")
	}
	if _, err := b.d.AnswerClaudeQuestion(ctx, item.ID, final, chatID); err != nil {
		b.edit(ctx, chatID, messageID, "Claude input failed: "+err.Error(), nil)
		return
	}
	b.edit(ctx, chatID, messageID, "Claude input sent.", nil)
}

func cloneAnswers(in map[string][]string) map[string][]string {
	out := map[string][]string{}
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}
func toggleAnswer(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return append(values, target)
}
func claudeOptionExists(q claudeQuestionSpec, value string) bool {
	for _, option := range q.Options {
		if option.Label == value {
			return true
		}
	}
	return false
}

func (b *telegramBridge) sendScreen(ctx context.Context, chatID int64, sessionID, caption string) {
	if strings.TrimSpace(sessionID) == "" {
		b.send(ctx, chatID, "No active session. Use /new shell, /new codex, /new pi, or /new claude.", nil)
		return
	}
	if b.isSessionEnded(ctx, sessionID) {
		b.send(ctx, chatID, b.sessionEndedText(ctx, chatID, sessionID), nil)
		return
	}
	s, err := b.d.sessionByID(sessionID)
	if err != nil {
		b.send(ctx, chatID, "Screen unavailable: session not found. Use /sessions or /new.", nil)
		return
	}
	if s.Transport == "codex" && len(s.Buf.Snapshot()) == 0 {
		b.send(ctx, chatID, "Codex has no activity yet. Send a normal message to start a turn.", b.sessionControls(ctx, s.ID))
		return
	}
	b.d.InvalidateSessionFrame(sessionID)
	b.queueScreen(ctx, chatID, sessionID, caption, true, time.Now(), true)
}
func (b *telegramBridge) sessionEndedText(ctx context.Context, chatID int64, sessionID string) string {
	name := "Selected session"
	agent := "session"
	if s, err := b.d.Registry.Get(sessionID); err == nil {
		name, agent = s.Name, s.Agent
	} else if b.d.DB != nil {
		if entry, found, err := b.d.DB.Session(ctx, sessionID); err == nil && found && entry.Ended {
			name, agent = entry.Name, entry.Agent
		}
	}
	if b.target(ctx, chatID) == sessionID {
		b.setTarget(ctx, chatID, "")
	}
	return name + " ended. Screens, input, and controls are unavailable. Use /new " + agent + " to start another."
}

func (b *telegramBridge) isSessionEnded(ctx context.Context, sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	if s, err := b.d.Registry.Get(sessionID); err == nil {
		return s.Ended()
	}
	if b.d.DB == nil {
		return false
	}
	entry, found, err := b.d.DB.Session(ctx, sessionID)
	return err == nil && found && entry.Ended
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
func (b *telegramBridge) edit(ctx context.Context, chat, message int64, text string, markup *telegram.InlineKeyboardMarkup) error {
	if _, err := b.client.EditMessageText(ctx, chat, message, boundedText(text), markup); err != nil {
		b.d.Log.Warn("Telegram edit", "chat", chat, "err", err)
		return err
	}
	return nil
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
	return "Onibi\n\n/new shell|codex|pi|claude [--name name] [--cwd path]\n/sessions\n/target <id|name>\n/tail [lines]\n/screen\n/font\n/paste\n/keys\n/key <name>\n/size small|medium|large\n/interrupt\n/esc\n/enter\n/kill\n\nNormal text sends literal input followed by Enter. Documents are privately staged for the selected session. Unknown /commands go to the selected session; // forces a command through when it conflicts with Onibi. /paste makes exactly the next message literal without Enter."
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
