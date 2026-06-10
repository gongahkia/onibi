package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/telegram"
)

func (d *Daemon) handleTextCommand(ctx context.Context, api telegram.API, m *models.Message) bool {
	cmd, arg, ok := parseTelegramCommand(m.Text)
	if !ok {
		return false
	}
	switch cmd {
	case "/sessions":
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: d.sessionsText()})
	case "/status":
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: d.statusText(ctx)})
	case "/help":
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: helpText()})
	case "/text":
		d.handleRenderOverride(ctx, api, m.Chat.ID, arg, render.ModeText)
	case "/screenshot":
		d.handleRenderOverride(ctx, api, m.Chat.ID, arg, render.ModePNG)
	case "/new", "/snooze", "/unsnooze", "/log":
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: strings.TrimPrefix(cmd, "/") + " is not implemented yet."})
	default:
		return false
	}
	return true
}

func parseTelegramCommand(text string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", "", false
	}
	cmd := fields[0]
	if at := strings.IndexByte(cmd, '@'); at >= 0 {
		cmd = cmd[:at]
	}
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), fields[0]))
	return strings.ToLower(cmd), arg, true
}

func (d *Daemon) handleRenderOverride(ctx context.Context, api telegram.API, chatID int64, target string, mode render.Mode) {
	s, msg := d.resolveSessionTarget(target)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	d.setRenderOverride(s.ID, mode)
	sendMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("%s output forced for %s (%s).", mode, s.Name, s.ID),
	})
}

func (d *Daemon) resolveSessionTarget(target string) (*Session, string) {
	live := d.liveSessions()
	if len(live) == 0 {
		return nil, "No active sessions."
	}
	if strings.TrimSpace(target) == "" {
		if len(live) == 1 {
			return live[0], ""
		}
		return nil, "Multiple active sessions. Use /sessions, then /text <id|name> or /screenshot <id|name>."
	}
	target = strings.TrimSpace(target)
	var matches []*Session
	for _, s := range live {
		if s.ID == target || strings.HasPrefix(s.ID, target) || s.Name == target {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		return matches[0], ""
	}
	if len(matches) > 1 {
		return nil, "Session target is ambiguous. Use the full session id."
	}
	return nil, "Session not found. Use /sessions."
}

func (d *Daemon) liveSessions() []*Session {
	all := d.Registry.List()
	out := make([]*Session, 0, len(all))
	for _, s := range all {
		if !s.Ended() {
			out = append(out, s)
		}
	}
	return out
}

func (d *Daemon) sessionsText() string {
	live := d.liveSessions()
	if len(live) == 0 {
		return "No active sessions."
	}
	var b strings.Builder
	b.WriteString("Active sessions:\n")
	for _, s := range live {
		fmt.Fprintf(&b, "%s  %s  %s  age=%s\n", s.ID, s.Name, s.Agent, time.Since(s.StartedAt()).Truncate(time.Second))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (d *Daemon) statusText(ctx context.Context) string {
	pending := "unknown"
	if d.Queue != nil {
		if p, err := d.Queue.Pending(ctx); err == nil {
			pending = fmt.Sprintf("%d", len(p))
		}
	}
	return fmt.Sprintf("Onibi status\nuptime=%s\nsessions=%d\npending_approvals=%s",
		time.Since(d.started).Truncate(time.Second), len(d.liveSessions()), pending)
}

func helpText() string {
	return strings.Join([]string{
		"Onibi commands:",
		"/sessions - list active sessions",
		"/status - show daemon status",
		"/text <id|name> - force text output",
		"/screenshot <id|name> - force screenshots",
		"/help - show this help",
	}, "\n")
}

func (d *Daemon) setRenderOverride(sessionID string, mode render.Mode) {
	d.renderMu.Lock()
	defer d.renderMu.Unlock()
	if mode == render.ModeText || mode == render.ModePNG {
		d.renderOverrides[sessionID] = mode
		return
	}
	delete(d.renderOverrides, sessionID)
}

func (d *Daemon) renderOverride(sessionID string) render.Mode {
	d.renderMu.RLock()
	defer d.renderMu.RUnlock()
	return d.renderOverrides[sessionID]
}
