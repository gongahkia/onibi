package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

func (d *Daemon) enqueuePromptText(ctx context.Context, api telegram.API, chatID int64, sessionID, text string) error {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return nil
	}
	s, err := d.resolveInjectTarget(ctx, chatID, sessionID)
	if errors.Is(err, errAmbiguousTarget) {
		d.queuePendingInject(chatID, text)
		d.sendTargetPicker(ctx, api, chatID, "Pick target session.")
		return nil
	}
	if errors.Is(err, ErrUnknownSession) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "No active PTY session."})
		return nil
	}
	if err != nil {
		return err
	}
	if d.DB == nil {
		return d.writePromptToSession(ctx, api, chatID, s, text, "")
	}
	p, err := d.DB.PromptEnqueue(ctx, s.ID, chatID, text)
	if err != nil {
		return err
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	d.audit(ctx, "prompt.queued", s.ID, text, chatID, "id="+p.ID)
	if d.encryptedModeEnabled() {
		_, _ = d.sendEncryptedText(ctx, api, chatID, "prompt", "Prompt queued", fmt.Sprintf("Queued prompt %s for %s (%s), position %d.", p.ID, s.Name, s.ID, p.Position))
		return d.dispatchNextPrompt(ctx, api, s)
	}
	sendAwaitingMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        fmt.Sprintf("Queued prompt %s for %s (%s), position %d.", p.ID, s.Name, s.ID, p.Position),
		ReplyMarkup: telegram.PromptKeyboard(p.ID),
	})
	return d.dispatchNextPrompt(ctx, api, s)
}

func (d *Daemon) dispatchNextPrompt(ctx context.Context, api telegram.API, s *Session) error {
	if s == nil || d.DB == nil {
		return nil
	}
	d.threadMu.RLock()
	busy := d.busySessions[s.ID]
	d.threadMu.RUnlock()
	if busy {
		return nil
	}
	p, ok, err := d.DB.PromptNext(ctx, s.ID)
	if err != nil || !ok {
		return err
	}
	if err := d.writePromptToSession(ctx, api, p.ChatID, s, p.Text, p.ID); err != nil {
		_, _ = d.DB.PromptSetState(ctx, p.ID, store.PromptFailed)
		return err
	}
	if _, err := d.DB.PromptSetState(ctx, p.ID, store.PromptSent); err != nil {
		return err
	}
	d.threadMu.Lock()
	d.busySessions[s.ID] = true
	d.threadMu.Unlock()
	d.audit(ctx, "prompt.sent", s.ID, p.Text, p.ChatID, "id="+p.ID)
	return nil
}

func (d *Daemon) writePromptToSession(ctx context.Context, api telegram.API, chatID int64, s *Session, text, promptID string) error {
	if s.Host == nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Session has no writable PTY."})
		return errors.New("session has no writable PTY")
	}
	payload := text
	if !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}
	if _, err := s.Host.Write([]byte(payload)); err != nil {
		return fmt.Errorf("write PTY: %w", err)
	}
	s.Touch()
	d.noteAnomaly(ctx, "telegram.inject")
	d.setDefaultTarget(ctx, chatID, s.ID)
	detail := "Sent to " + s.Name + " (" + s.ID + ")."
	if promptID != "" {
		detail = "Sent prompt " + promptID + " to " + s.Name + " (" + s.ID + ")."
	}
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "prompt", "Prompt sent", detail)
	return nil
}

func (d *Daemon) markSessionReady(ctx context.Context, api telegram.API, s *Session) {
	if s == nil {
		return
	}
	d.threadMu.Lock()
	delete(d.busySessions, s.ID)
	d.threadMu.Unlock()
	_ = d.dispatchNextPrompt(ctx, api, s)
}

func (d *Daemon) handleQueueCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	if d.DB == nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "No prompt DB."})
		return
	}
	sessionID := ""
	if strings.TrimSpace(arg) != "" {
		s, msg := d.resolveSessionTarget(arg)
		if msg != "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
			return
		}
		sessionID = s.ID
	}
	rows, err := d.DB.PromptList(ctx, sessionID, false, 20)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Queue read failed: " + err.Error()})
		return
	}
	if len(rows) == 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Prompt queue empty."})
		return
	}
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "queue", "Prompt queue", promptListText(rows))
}

func (d *Daemon) handlePromptCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	if strings.TrimSpace(arg) == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /prompt <text>"})
		return
	}
	if err := d.enqueuePromptText(ctx, api, chatID, "", arg); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Prompt failed: " + err.Error()})
	}
}

func (d *Daemon) handleEditPromptCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	id, text, ok := splitIDRest(arg)
	if !ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /editprompt <prompt_id> <text>"})
		return
	}
	d.applyPromptEdit(ctx, api, chatID, id, text)
}

func (d *Daemon) handleCancelPromptCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	id := strings.TrimSpace(arg)
	if id == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /cancelprompt <prompt_id>"})
		return
	}
	if _, err := d.DB.PromptSetState(ctx, id, store.PromptCancelled); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Cancel failed: " + err.Error()})
		return
	}
	d.audit(ctx, "prompt.cancelled", "", "", chatID, "id="+id)
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Cancelled prompt " + id + "."})
}

func (d *Daemon) handleMovePromptCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	fields := strings.Fields(arg)
	if len(fields) != 2 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /moveprompt <prompt_id> <position>"})
		return
	}
	pos, err := strconv.Atoi(fields[1])
	if err != nil || pos <= 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Position must be a positive integer."})
		return
	}
	p, err := d.DB.PromptMove(ctx, fields[0], pos)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Move failed: " + err.Error()})
		return
	}
	d.audit(ctx, "prompt.moved", p.SessionID, "", chatID, fmt.Sprintf("id=%s pos=%d", p.ID, p.Position))
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Moved %s to position %d.", p.ID, p.Position)})
}

func (d *Daemon) handleFlushQueueCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	sessionID := ""
	if strings.TrimSpace(arg) != "" {
		s, msg := d.resolveSessionTarget(arg)
		if msg != "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
			return
		}
		sessionID = s.ID
	}
	n, err := d.DB.PromptCancelQueued(ctx, sessionID)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Flush failed: " + err.Error()})
		return
	}
	d.audit(ctx, "prompt.flush", sessionID, "", chatID, fmt.Sprintf("%d prompt(s)", n))
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Cancelled %d queued prompt(s).", n)})
}

func (d *Daemon) handlePromptCallback(ctx context.Context, api telegram.API, q *models.CallbackQuery, verb, id string) error {
	switch verb {
	case "prompt_send":
		p, err := d.DB.PromptMove(ctx, id, 1)
		if err != nil {
			answerCallback(ctx, api, q.ID, "Prompt unavailable")
			return nil
		}
		s, err := d.sessionByID(p.SessionID)
		if err != nil {
			answerCallback(ctx, api, q.ID, "Session unavailable")
			return nil
		}
		answerCallback(ctx, api, q.ID, "Queued at front")
		return d.dispatchNextPrompt(ctx, api, s)
	case "prompt_edit":
		if d.encryptedModeEnabled() {
			answerCallback(ctx, api, q.ID, "Use secure controls")
			d.sendSecureRequired(ctx, api, q.From.ID)
			return nil
		}
		d.editMu.Lock()
		d.pendingPromptEdits[q.From.ID] = id
		d.editMu.Unlock()
		answerCallback(ctx, api, q.ID, "Send replacement text")
		sendAwaitingMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: "Reply with replacement prompt text for " + id + ". Reply 'cancel' to abort."})
		return nil
	case "prompt_cancel":
		if _, err := d.DB.PromptSetState(ctx, id, store.PromptCancelled); err != nil {
			answerCallback(ctx, api, q.ID, "Cancel failed")
			return nil
		}
		d.audit(ctx, "prompt.cancelled", "", "", q.From.ID, "id="+id)
		answerCallback(ctx, api, q.ID, "Cancelled")
		return nil
	case "prompt_up", "prompt_down":
		delta := -1
		if verb == "prompt_down" {
			delta = 1
		}
		p, err := d.DB.PromptMoveRelative(ctx, id, delta)
		if err != nil {
			answerCallback(ctx, api, q.ID, "Move failed")
			return nil
		}
		answerCallback(ctx, api, q.ID, fmt.Sprintf("Position %d", p.Position))
		return nil
	}
	return nil
}

func (d *Daemon) applyPromptEdit(ctx context.Context, api telegram.API, chatID int64, id, text string) {
	if strings.TrimSpace(text) == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Replacement prompt is empty."})
		return
	}
	p, err := d.DB.PromptUpdateText(ctx, id, text)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Edit failed: " + err.Error()})
		return
	}
	d.audit(ctx, "prompt.edited", p.SessionID, text, chatID, "id="+id)
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "prompt", "Prompt edited", "Edited prompt "+id+".")
}

func (d *Daemon) handlePendingPromptEdit(ctx context.Context, api telegram.API, m *models.Message) bool {
	d.editMu.Lock()
	id, ok := d.pendingPromptEdits[m.From.ID]
	if ok {
		delete(d.pendingPromptEdits, m.From.ID)
	}
	d.editMu.Unlock()
	if !ok {
		return false
	}
	if d.encryptedModeEnabled() {
		d.sendSecureRequired(ctx, api, m.Chat.ID)
		return true
	}
	text := strings.TrimSpace(m.Text)
	if strings.EqualFold(text, "cancel") {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Prompt edit cancelled."})
		return true
	}
	d.applyPromptEdit(ctx, api, m.Chat.ID, id, m.Text)
	return true
}

func promptListText(rows []store.PromptEntry) string {
	var b strings.Builder
	b.WriteString("Queued prompts:\n")
	for _, p := range rows {
		fmt.Fprintf(&b, "%s  %s  pos=%d  %s\n", p.ID, shortID(p.SessionID), p.Position, promptPreview(p.Text))
		if b.Len() > 3800 {
			b.WriteString("...")
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func promptPreview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= 80 {
		return s
	}
	return string(r[:77]) + "..."
}

func splitIDRest(s string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 2 {
		return "", "", false
	}
	id := fields[0]
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), id))
	return id, rest, rest != ""
}
