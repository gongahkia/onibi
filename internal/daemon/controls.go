package daemon

import (
	"context"
	"fmt"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/telegram"
)

func (d *Daemon) handlePeekCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	d.sendSessionPreview(ctx, api, chatID, s)
}

func (d *Daemon) handleInterruptCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	var authMsg string
	var ok bool
	arg, authMsg, ok = d.requireTOTP(ctx, arg)
	if !ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: authMsg})
		return
	}
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	if s.Host == nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Session has no writable PTY."})
		return
	}
	if _, err := s.Host.Write([]byte{3}); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Interrupt failed: " + err.Error()})
		return
	}
	d.threadMu.Lock()
	delete(d.busySessions, s.ID)
	d.threadMu.Unlock()
	d.audit(ctx, "session.interrupt", s.ID, "", chatID, "ctrl-c")
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "session", "Session interrupted", "Interrupted "+s.Name+" ("+s.ID+").")
}

func (d *Daemon) handleKillCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	var authMsg string
	var ok bool
	arg, authMsg, ok = d.requireTOTP(ctx, arg)
	if !ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: authMsg})
		return
	}
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	if s.Host != nil {
		_ = s.Host.Close()
	}
	d.markSessionEnded(ctx, s)
	d.audit(ctx, "session.kill", s.ID, "", chatID, "telegram")
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "session", "Session killed", "Killed "+s.Name+" ("+s.ID+").")
}

func (d *Daemon) handleRenameCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	target, name, ok := splitIDRest(arg)
	if !ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /rename <id|name> <new name>"})
		return
	}
	s, msg := d.resolveSessionTarget(target)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "New name is empty."})
		return
	}
	s.Name = name
	if d.DB != nil {
		_ = d.DB.SessionRename(ctx, s.ID, name)
		d.audit(ctx, "session.rename", s.ID, "", chatID, "name="+name)
	}
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "session", "Session renamed", fmt.Sprintf("Renamed %s to %s.", s.ID, name))
}

func (d *Daemon) handleMenuCommand(ctx context.Context, api telegram.API, chatID int64) {
	if d.encryptedModeEnabled() {
		d.sendSecureRequired(ctx, api, chatID)
		return
	}
	live := d.liveSessions()
	if len(live) == 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "No active sessions."})
		return
	}
	targets := make([]telegram.SessionTarget, 0, len(live))
	for _, s := range live {
		targets = append(targets, telegram.SessionTarget{ID: s.ID, Label: s.Name + " " + s.Agent + " " + s.ID})
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        "Session menu",
		ReplyMarkup: telegram.SessionMenuKeyboard(targets),
	})
}

func (d *Daemon) handleSessionActionCallback(ctx context.Context, api telegram.API, q *models.CallbackQuery, verb, id string) error {
	switch verb {
	case "peek":
		s, err := d.sessionByID(id)
		if err != nil {
			answerCallback(ctx, api, q.ID, "Session unavailable")
			return nil
		}
		answerCallback(ctx, api, q.ID, "Sending preview")
		d.sendSessionPreview(ctx, api, q.From.ID, s)
	case "interrupt":
		if enabled, msg := d.totpEnabled(ctx); enabled {
			answerCallback(ctx, api, q.ID, "TOTP required")
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: msg})
			return nil
		}
		d.handleInterruptCommand(ctx, api, q.From.ID, id)
		answerCallback(ctx, api, q.ID, "Interrupted")
	case "kill":
		if enabled, msg := d.totpEnabled(ctx); enabled {
			answerCallback(ctx, api, q.ID, "TOTP required")
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: msg})
			return nil
		}
		d.handleKillCommand(ctx, api, q.From.ID, id)
		answerCallback(ctx, api, q.ID, "Killed")
	}
	return nil
}
