package daemon

import (
	"context"
	"errors"
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

func (d *Daemon) clearDefaultTargetsForSession(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	d.threadMu.Lock()
	for chatID, sid := range d.defaultTargets {
		if sid == sessionID {
			delete(d.defaultTargets, chatID)
		}
	}
	d.threadMu.Unlock()
	if d.DB == nil {
		return
	}
	keys, err := d.DB.KVKeysWithPrefix(ctx, "target:")
	if err != nil {
		return
	}
	for _, key := range keys {
		sid, ok, err := d.DB.KVGetString(ctx, key)
		if err == nil && ok && sid == sessionID {
			_ = d.DB.KVDel(ctx, key)
		}
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
	if strings.TrimSpace(id) == "" {
		return nil, ErrUnknownSession
	}
	var nameMatches []*Session
	for _, s := range d.liveSessions() {
		if s.ID == id || strings.HasPrefix(s.ID, id) {
			return s, nil
		}
		if s.Name == id {
			nameMatches = append(nameMatches, s)
		}
	}
	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		return nil, errAmbiguousTarget
	}
	return nil, ErrUnknownSession
}

func (d *Daemon) injectTelegramText(ctx context.Context, api telegram.API, chatID int64, sessionID, text string) error {
	return d.enqueuePromptText(ctx, api, chatID, sessionID, text)
}

func (d *Daemon) queuePendingInject(ctx context.Context, chatID int64, text string) {
	d.setPending(ctx, pendingKindInject, chatID, text)
}

func (d *Daemon) popPendingInject(ctx context.Context, chatID int64) string {
	text, _ := d.takePending(ctx, pendingKindInject, chatID)
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
	sendAwaitingMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: telegram.SessionTargetKeyboard(targets),
	})
}
