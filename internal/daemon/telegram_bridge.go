package daemon

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/tmux"
)

const (
	TelegramSecretBotToken = "TELEGRAM_BOT_TOKEN"
	TelegramKVOwnerChatID  = "telegram.owner_chat_id"
	TelegramKVPairCode     = "telegram.pair_code"
	telegramTargetPrefix   = "telegram.target."
)

type telegramBridge struct {
	d      *Daemon
	client *telegram.Client

	mu        sync.Mutex
	ownerID   int64
	seen      map[string]bool
	killArmed map[int64]time.Time
}

func (d *Daemon) runTelegramBridge(ctx context.Context) error {
	c := telegram.NewClient(d.TelegramToken)
	if err := c.DeleteWebhook(ctx); err != nil {
		d.Log.Warn("telegram delete webhook failed", "err", err)
	}
	b := &telegramBridge{
		d:         d,
		client:    c,
		ownerID:   d.TelegramOwnerID,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	go b.forwardApprovals(ctx)
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := c.GetUpdates(ctx, offset, 25)
		if err != nil {
			d.Log.Warn("telegram poll failed", slog.Any("err", err))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			b.handleUpdate(ctx, u)
		}
	}
}

func (b *telegramBridge) handleUpdate(ctx context.Context, u telegram.Update) {
	if u.CallbackQuery != nil {
		b.handleCallback(ctx, u.CallbackQuery)
		return
	}
	if u.Message == nil {
		return
	}
	m := u.Message
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}
	if !b.authorizedOrPair(ctx, m) {
		return
	}
	if strings.HasPrefix(text, "/") {
		b.handleCommand(ctx, m.Chat.ID, text)
		return
	}
	out, err := b.d.SendSessionTextAndCapture(ctx, b.target(ctx, m.Chat.ID), text, true)
	if err != nil {
		b.send(ctx, m.Chat.ID, "Input failed: "+err.Error(), nil)
		return
	}
	b.sendChunks(ctx, m.Chat.ID, b.d.prepareProviderOutput(out))
}

func (b *telegramBridge) authorizedOrPair(ctx context.Context, m *telegram.Message) bool {
	chatID := m.Chat.ID
	if b.owner() == chatID {
		return true
	}
	text := strings.TrimSpace(m.Text)
	if strings.HasPrefix(strings.ToLower(text), "/start") {
		arg := strings.TrimSpace(strings.TrimPrefix(text, strings.Fields(text)[0]))
		if arg != "" && arg == strings.TrimSpace(b.d.TelegramPair) {
			b.setOwner(ctx, chatID)
			b.send(ctx, chatID, "Paired. Send /new shell or text an active session.", nil)
			return true
		}
	}
	if b.owner() == 0 {
		b.send(ctx, chatID, "Onibi is not paired. Run `onibi up --transport=telegram` and send /start <code> from that output.", nil)
	}
	return false
}

func (b *telegramBridge) owner() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ownerID
}

func (b *telegramBridge) setOwner(ctx context.Context, chatID int64) {
	b.mu.Lock()
	b.ownerID = chatID
	b.mu.Unlock()
	b.d.TelegramOwnerID = chatID
	if b.d.DB != nil {
		_ = b.d.DB.KVSetString(ctx, TelegramKVOwnerChatID, strconv.FormatInt(chatID, 10))
		_ = b.d.DB.KVDel(ctx, TelegramKVPairCode)
	}
}

func (b *telegramBridge) handleCommand(ctx context.Context, chatID int64, text string) {
	cmd, arg := splitTelegramCommand(text)
	switch cmd {
	case "/start", "/help":
		b.send(ctx, chatID, telegramHelp(), nil)
	case "/status", "/ping":
		b.send(ctx, chatID, b.d.pingText(ctx, -1)+"\n\n"+b.sessionsText(ctx, chatID), nil)
	case "/sessions":
		b.send(ctx, chatID, b.sessionsText(ctx, chatID), nil)
	case "/target":
		s, err := b.d.sessionByID(strings.TrimSpace(arg))
		if err != nil {
			b.send(ctx, chatID, "Target failed: "+err.Error(), nil)
			return
		}
		b.setTarget(ctx, chatID, s.ID)
		b.send(ctx, chatID, "Target: "+s.Name+" ("+s.ID+")", nil)
	case "/new":
		b.handleNew(ctx, chatID, arg)
	case "/peek", "/text":
		out, err := b.d.CaptureSessionText(ctx, b.target(ctx, chatID))
		if err != nil {
			b.send(ctx, chatID, "Peek failed: "+err.Error(), nil)
			return
		}
		b.sendChunks(ctx, chatID, out)
	case "/render", "/screenshot":
		b.handleRender(ctx, chatID)
	case "/show":
		msg, err := b.d.ShowSession(ctx, arg)
		if err != nil {
			b.send(ctx, chatID, "Show failed: "+err.Error(), nil)
			return
		}
		b.send(ctx, chatID, msg, nil)
	case "/hide":
		msg, err := b.d.HideSession(ctx, b.target(ctx, chatID), "headless")
		if err != nil {
			b.send(ctx, chatID, "Hide failed: "+err.Error(), nil)
			return
		}
		b.send(ctx, chatID, msg, nil)
	case "/end":
		msg, err := b.d.HideSession(ctx, b.target(ctx, chatID), "end")
		if err != nil {
			b.send(ctx, chatID, "End failed: "+err.Error(), nil)
			return
		}
		b.send(ctx, chatID, msg, nil)
	case "/interrupt":
		if err := b.d.ControlSession(ctx, b.target(ctx, chatID), "interrupt"); err != nil {
			b.send(ctx, chatID, "Interrupt failed: "+err.Error(), nil)
			return
		}
		b.send(ctx, chatID, "Interrupted.", nil)
	case "/esc":
		if err := b.d.SendSessionKey(ctx, b.target(ctx, chatID), "Escape"); err != nil {
			b.send(ctx, chatID, "Esc failed: "+err.Error(), nil)
			return
		}
		b.send(ctx, chatID, "Esc sent.", nil)
	case "/enter":
		if err := b.d.SendSessionKey(ctx, b.target(ctx, chatID), "Enter"); err != nil {
			b.send(ctx, chatID, "Enter failed: "+err.Error(), nil)
			return
		}
		b.send(ctx, chatID, "Enter sent.", nil)
	case "/kill":
		b.handleKill(ctx, chatID)
	case "/approve":
		b.decideApproval(ctx, chatID, strings.TrimSpace(arg), approval.VerdictApprove, "", "")
	case "/deny":
		id, reason, _ := strings.Cut(strings.TrimSpace(arg), " ")
		if reason == "" {
			reason = "denied from Telegram"
		}
		b.decideApproval(ctx, chatID, id, approval.VerdictDeny, "", reason)
	case "/edit":
		id, edited, _ := strings.Cut(strings.TrimSpace(arg), " ")
		b.decideApproval(ctx, chatID, id, approval.VerdictEdit, edited, "")
	default:
		b.send(ctx, chatID, "Unknown command. Send /help.", nil)
	}
}

func splitTelegramCommand(text string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", ""
	}
	cmd := strings.ToLower(fields[0])
	if at := strings.IndexByte(cmd, '@'); at >= 0 {
		cmd = cmd[:at]
	}
	return cmd, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), fields[0]))
}

func (b *telegramBridge) handleNew(ctx context.Context, chatID int64, arg string) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		fields = []string{"shell"}
	}
	agent := strings.ToLower(fields[0])
	bin, spawnAgent, spawnArgs, ok := agentCommand(agent, fields[1:])
	if !ok {
		b.send(ctx, chatID, "Unsupported target. Try /new shell, /new claude, /new codex.", nil)
		return
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		b.send(ctx, chatID, bin+" not found in PATH", nil)
		return
	}
	s, err := b.d.StartTmuxSession(ctx, "", spawnAgent, path, spawnArgs, "")
	if err != nil {
		b.send(ctx, chatID, "Start failed: "+err.Error(), nil)
		return
	}
	b.setTarget(ctx, chatID, s.ID)
	b.send(ctx, chatID, "Started "+s.Name+" ("+s.ID+"). Text this chat to send input.", nil)
}

func (b *telegramBridge) handleRender(ctx context.Context, chatID int64) {
	s, err := b.d.sessionForRPCTarget(b.target(ctx, chatID))
	if err != nil {
		b.send(ctx, chatID, "Render failed: "+err.Error(), nil)
		return
	}
	buf := s.Buf.Snapshot()
	png, err := render.RenderPNG(buf, render.PNGOptions{Rows: 40, Cols: 100, Scale: 1})
	if err != nil {
		b.send(ctx, chatID, "Render failed: "+err.Error(), nil)
		return
	}
	if err := b.client.SendPhoto(ctx, chatID, png, s.Name+" ("+s.ID+")"); err != nil {
		b.send(ctx, chatID, "Send photo failed: "+err.Error(), nil)
	}
}

func (b *telegramBridge) handleKill(ctx context.Context, chatID int64) {
	b.mu.Lock()
	armed := b.killArmed[chatID]
	if time.Since(armed) > 2*time.Second {
		b.killArmed[chatID] = time.Now()
		b.mu.Unlock()
		b.send(ctx, chatID, "Send /kill again within 2s to kill the target.", nil)
		return
	}
	delete(b.killArmed, chatID)
	b.mu.Unlock()
	if err := b.d.ControlSession(ctx, b.target(ctx, chatID), "kill"); err != nil {
		b.send(ctx, chatID, "Kill failed: "+err.Error(), nil)
		return
	}
	b.send(ctx, chatID, "Killed.", nil)
}

func (b *telegramBridge) handleCallback(ctx context.Context, q *telegram.CallbackQuery) {
	chatID := int64(0)
	if q.Message != nil {
		chatID = q.Message.Chat.ID
	}
	if chatID == 0 || b.owner() != chatID {
		_ = b.client.AnswerCallbackQuery(ctx, q.ID, "not authorized")
		return
	}
	parts := strings.SplitN(q.Data, ":", 2)
	if len(parts) != 2 {
		_ = b.client.AnswerCallbackQuery(ctx, q.ID, "bad action")
		return
	}
	switch parts[0] {
	case "ap":
		a, err := b.d.Queue.Get(ctx, parts[1])
		if err != nil {
			_ = b.client.AnswerCallbackQuery(ctx, q.ID, err.Error())
			return
		}
		if approval.ClassifyRisk(a.Tool, a.InputJSON).Level == "high" {
			b.send(ctx, chatID, "High-risk approval. Tap confirm to approve "+a.ID+".", &telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Confirm approve", CallbackData: "cf:" + a.ID}, {Text: "Deny", CallbackData: "dn:" + a.ID}}},
			})
			_ = b.client.AnswerCallbackQuery(ctx, q.ID, "confirm required")
			return
		}
		b.decideApproval(ctx, chatID, parts[1], approval.VerdictApprove, "", "")
	case "cf":
		b.decideApproval(ctx, chatID, parts[1], approval.VerdictApprove, "", "")
	case "dn":
		b.decideApproval(ctx, chatID, parts[1], approval.VerdictDeny, "", "denied from Telegram")
	default:
		_ = b.client.AnswerCallbackQuery(ctx, q.ID, "unknown action")
		return
	}
	_ = b.client.AnswerCallbackQuery(ctx, q.ID, "ok")
}

func (b *telegramBridge) decideApproval(ctx context.Context, chatID int64, id string, verdict approval.Verdict, edited, reason string) {
	id = strings.TrimSpace(id)
	if id == "" {
		b.send(ctx, chatID, "Approval id required.", nil)
		return
	}
	if verdict == approval.VerdictEdit {
		a, err := b.d.Queue.Get(ctx, id)
		if err != nil {
			b.send(ctx, chatID, "Edit failed: "+err.Error(), nil)
			return
		}
		if edited == "" {
			b.send(ctx, chatID, "Usage: /edit "+id+" <edited JSON>", nil)
			return
		}
		if err := approval.ValidateEditedInput(a.Tool, a.InputJSON, edited); err != nil {
			b.send(ctx, chatID, "Edit failed: "+err.Error(), nil)
			return
		}
	}
	if err := b.d.Queue.Decide(ctx, id, verdict, edited, reason, chatID); err != nil {
		b.send(ctx, chatID, "Approval failed: "+err.Error(), nil)
		return
	}
	b.send(ctx, chatID, "Approval "+id+": "+string(verdict), nil)
}

func (b *telegramBridge) forwardApprovals(ctx context.Context) {
	ch, unsub := b.d.Queue.Subscribe()
	defer unsub()
	if b.owner() != 0 {
		if pending, err := b.d.Queue.Pending(ctx); err == nil {
			for _, a := range pending {
				b.sendApproval(ctx, a)
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Type == approval.EventRequested {
				a := ev.Approval
				b.sendApproval(ctx, &a)
			}
		}
	}
}

func (b *telegramBridge) sendApproval(ctx context.Context, a *approval.Approval) {
	if a == nil || b.owner() == 0 {
		return
	}
	b.mu.Lock()
	if b.seen[a.ID] {
		b.mu.Unlock()
		return
	}
	b.seen[a.ID] = true
	b.mu.Unlock()
	text := formatApprovalWithPolicy(a, b.d.ProviderOutput)
	markup := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "Approve", CallbackData: "ap:" + a.ID}, {Text: "Deny", CallbackData: "dn:" + a.ID}},
	}}
	b.send(ctx, b.owner(), text, markup)
}

func formatApproval(a *approval.Approval) string {
	return formatApprovalWithPolicy(a, ProviderOutputPolicy{})
}

func formatApprovalWithPolicy(a *approval.Approval, policy ProviderOutputPolicy) string {
	details := approval.ExtractDetails(a.Tool, a.InputJSON)
	var b strings.Builder
	fmt.Fprintf(&b, "Approval %s\nagent=%s tool=%s session=%s\nrisk=%s\n", a.ID, a.Agent, a.Tool, a.SessionID, approval.ClassifyRisk(a.Tool, a.InputJSON).Level)
	if details.Command != "" {
		fmt.Fprintf(&b, "\ncommand:\n%s\n", policy.redact(details.Command))
	}
	if details.FilePath != "" {
		fmt.Fprintf(&b, "\nfile:\n%s\n", policy.redact(details.FilePath))
	}
	body := policy.redact(a.InputJSON)
	if len(body) > 1800 {
		body = body[:1800] + "\n..."
	}
	fmt.Fprintf(&b, "\ninput:\n%s\n\nEdit: /edit %s <edited JSON>", body, a.ID)
	return b.String()
}

func (b *telegramBridge) sessionsText(ctx context.Context, chatID int64) string {
	live := b.d.liveSessions()
	if len(live) == 0 {
		return "No active sessions. Try /new shell or /new claude."
	}
	target := b.target(ctx, chatID)
	var out strings.Builder
	out.WriteString("Sessions:\n")
	for _, s := range live {
		mark := " "
		if target == "" && len(live) == 1 || target == s.ID {
			mark = "*"
		}
		fmt.Fprintf(&out, "%s %s  %s  %s\n", mark, s.ID, s.Name, s.CWD)
	}
	return strings.TrimRight(out.String(), "\n")
}

func (b *telegramBridge) target(ctx context.Context, chatID int64) string {
	if b.d.DB == nil {
		return ""
	}
	v, ok, _ := b.d.DB.KVGetString(ctx, telegramTargetPrefix+strconv.FormatInt(chatID, 10))
	if ok {
		return v
	}
	return ""
}

func (b *telegramBridge) setTarget(ctx context.Context, chatID int64, id string) {
	if b.d.DB != nil {
		_ = b.d.DB.KVSetString(ctx, telegramTargetPrefix+strconv.FormatInt(chatID, 10), id)
	}
}

func (b *telegramBridge) sendChunks(ctx context.Context, chatID int64, text string) {
	for _, chunk := range telegram.ChunkText(text, 3800) {
		b.send(ctx, chatID, chunk, nil)
	}
}

func (b *telegramBridge) send(ctx context.Context, chatID int64, text string, markup *telegram.InlineKeyboardMarkup) {
	text = redactChatText(text)
	if _, err := b.client.SendMessage(ctx, chatID, text, markup); err != nil {
		b.d.Log.Warn("telegram send failed", "chat_id", chatID, "err", err)
	}
}

func telegramHelp() string {
	return strings.TrimSpace(`Onibi Telegram

Text this chat to send input to the current target.

/new shell|claude|codex|goose|gemini|opencode|copilot|pi|amp
/sessions
/target <id|name>
/peek
/render
/show
/hide
/end
/interrupt
/esc
/enter
/kill
/approve <id>
/deny <id> [reason]
/edit <id> <edited JSON>`)
}

func NewTelegramPairCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(b[:]) % 1000000
	return fmt.Sprintf("%06d", n), nil
}

func parseTelegramOwnerID(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func (d *Daemon) CaptureSessionText(ctx context.Context, id string) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		out, err := newTmuxController().Capture(ctx, s.TmuxTarget, 80)
		if err != nil {
			return "", err
		}
		s.Buf.Reset()
		_, _ = s.Buf.Write([]byte(out))
		return render.TextTailBody([]byte(out), render.Options{MaxLines: 40, MaxChars: 3500}), nil
	}
	return render.TextTailBody(s.Buf.Snapshot(), render.Options{MaxLines: 40, MaxChars: 3500}), nil
}

func (d *Daemon) SendSessionTextAndCapture(ctx context.Context, id, text string, enter bool) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text required")
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		ctrl := newTmuxController()
		if err := ctrl.SendText(ctx, s.TmuxTarget, text, enter); err != nil {
			return "", err
		}
		d.touchSession(ctx, s)
		return d.captureStableTmux(ctx, ctrl, s)
	}
	if s.Host == nil {
		return "", errors.New("session has no writable PTY")
	}
	payload := text
	if enter && !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}
	if _, err := s.Host.Write([]byte(payload)); err != nil {
		return "", err
	}
	d.touchSession(ctx, s)
	d.waitSessionIdle(ctx, s, 800*time.Millisecond, 5*time.Second)
	return d.CaptureSessionText(ctx, s.ID)
}

func (d *Daemon) captureStableTmux(ctx context.Context, ctrl *tmux.Controller, s *Session) (string, error) {
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	last := ""
	stable := 0
	for {
		out, err := ctrl.Capture(ctx, s.TmuxTarget, 80)
		if err != nil {
			return "", err
		}
		if out == last {
			stable++
			if stable >= 2 {
				s.Buf.Reset()
				_, _ = s.Buf.Write([]byte(out))
				d.touchSession(ctx, s)
				return render.TextTailBody([]byte(out), render.Options{MaxLines: 40, MaxChars: 3500}), nil
			}
		} else {
			stable = 0
			last = out
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			s.Buf.Reset()
			_, _ = s.Buf.Write([]byte(out))
			d.touchSession(ctx, s)
			return render.TextTailBody([]byte(out), render.Options{MaxLines: 40, MaxChars: 3500}), nil
		case <-tick.C:
		}
	}
}

func (d *Daemon) waitSessionIdle(ctx context.Context, s *Session, idle, max time.Duration) {
	deadline := time.NewTimer(max)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if s.SinceActivity() >= idle {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
	}
}

func (d *Daemon) SendSessionKey(ctx context.Context, id, key string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		return newTmuxController().SendKey(ctx, s.TmuxTarget, key)
	}
	if s.Host == nil {
		return errors.New("session has no writable PTY")
	}
	switch key {
	case "Enter":
		_, err = s.Host.Write([]byte{'\n'})
	case "Escape":
		_, err = s.Host.Write([]byte{0x1b})
	default:
		err = errors.New("unsupported key")
	}
	return err
}

func (d *Daemon) ControlSession(ctx context.Context, id, action string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		ctrl := newTmuxController()
		switch action {
		case "interrupt":
			return ctrl.SendKey(ctx, s.TmuxTarget, "C-c")
		case "kill":
			if err := ctrl.KillSession(ctx, s.TmuxTarget); err != nil {
				return err
			}
			d.markSessionEnded(ctx, s)
			return nil
		default:
			return errors.New("unsupported action")
		}
	}
	if s.Host == nil {
		return errors.New("session has no writable PTY")
	}
	switch action {
	case "interrupt":
		_, err = s.Host.Write([]byte{3})
		return err
	case "kill":
		if err := s.Host.Close(); err != nil {
			return err
		}
		d.markSessionEnded(ctx, s)
		return nil
	default:
		return errors.New("unsupported action")
	}
}

func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
