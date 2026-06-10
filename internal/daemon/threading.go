package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/telegram"
)

var errAmbiguousTarget = errors.New("ambiguous session target")

func (d *Daemon) bindMessage(msg *models.Message, sessionID string) {
	if msg == nil || sessionID == "" || msg.Chat.ID == 0 || msg.ID == 0 {
		return
	}
	d.threadMu.Lock()
	d.messageSessions[messageKey{chatID: msg.Chat.ID, msgID: msg.ID}] = sessionID
	d.threadMu.Unlock()
}

func (d *Daemon) sessionIDForReply(m *models.Message) string {
	if m == nil || m.ReplyToMessage == nil {
		return ""
	}
	d.threadMu.RLock()
	id := d.messageSessions[messageKey{chatID: m.Chat.ID, msgID: m.ReplyToMessage.ID}]
	d.threadMu.RUnlock()
	return id
}

func (d *Daemon) setDefaultTarget(ctx context.Context, chatID int64, sessionID string) {
	d.threadMu.Lock()
	d.defaultTargets[chatID] = sessionID
	d.threadMu.Unlock()
	if d.DB != nil {
		_ = d.DB.KVSetString(ctx, defaultTargetKey(chatID), sessionID)
	}
}

func (d *Daemon) clearDefaultTarget(ctx context.Context, chatID int64) {
	d.threadMu.Lock()
	delete(d.defaultTargets, chatID)
	d.threadMu.Unlock()
	if d.DB != nil {
		_ = d.DB.KVDel(ctx, defaultTargetKey(chatID))
	}
}

func (d *Daemon) defaultTarget(ctx context.Context, chatID int64) string {
	d.threadMu.RLock()
	id := d.defaultTargets[chatID]
	d.threadMu.RUnlock()
	if id != "" || d.DB == nil {
		return id
	}
	id, ok, err := d.DB.KVGetString(ctx, defaultTargetKey(chatID))
	if err == nil && ok {
		d.threadMu.Lock()
		d.defaultTargets[chatID] = id
		d.threadMu.Unlock()
		return id
	}
	return ""
}

func defaultTargetKey(chatID int64) string {
	return "target:" + strconv.FormatInt(chatID, 10)
}

func (d *Daemon) resolveInjectTarget(ctx context.Context, chatID int64, explicit string) (*Session, error) {
	if explicit != "" {
		return d.sessionByID(explicit)
	}
	if id := d.defaultTarget(ctx, chatID); id != "" {
		s, err := d.sessionByID(id)
		if err == nil {
			return s, nil
		}
		d.clearDefaultTarget(ctx, chatID)
	}
	live := d.liveSessions()
	if len(live) == 0 {
		return nil, ErrUnknownSession
	}
	if len(live) == 1 {
		return live[0], nil
	}
	return nil, errAmbiguousTarget
}

func (d *Daemon) sessionByID(id string) (*Session, error) {
	for _, s := range d.liveSessions() {
		if s.ID == id || strings.HasPrefix(s.ID, id) {
			return s, nil
		}
	}
	return nil, ErrUnknownSession
}

func (d *Daemon) injectTelegramText(ctx context.Context, api telegram.API, chatID int64, sessionID, text string) error {
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
	if s.Host == nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Session has no writable PTY."})
		return nil
	}
	payload := text
	if !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}
	if _, err := s.Host.Write([]byte(payload)); err != nil {
		return fmt.Errorf("write PTY: %w", err)
	}
	s.Touch()
	d.setDefaultTarget(ctx, chatID, s.ID)
	if d.DB != nil {
		_ = d.DB.AuditAppend(ctx, "session.inject", s.ID, payload, chatID, "telegram text")
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Sent to " + s.Name + " (" + s.ID + ")."})
	return nil
}

func (d *Daemon) queuePendingInject(chatID int64, text string) {
	d.threadMu.Lock()
	d.pendingInjects[chatID] = text
	d.threadMu.Unlock()
}

func (d *Daemon) popPendingInject(chatID int64) string {
	d.threadMu.Lock()
	defer d.threadMu.Unlock()
	text := d.pendingInjects[chatID]
	delete(d.pendingInjects, chatID)
	return text
}

func (d *Daemon) sendTargetPicker(ctx context.Context, api telegram.API, chatID int64, text string) {
	live := d.liveSessions()
	targets := make([]telegram.SessionTarget, 0, len(live))
	for _, s := range live {
		targets = append(targets, telegram.SessionTarget{
			ID:    s.ID,
			Label: s.Name + " " + s.Agent + " " + s.ID,
		})
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: telegram.SessionTargetKeyboard(targets),
	})
}
