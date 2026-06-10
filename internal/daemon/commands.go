package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	case "/target":
		d.handleTargetCommand(ctx, api, m.Chat.ID, arg)
	case "/new":
		d.handleNewCommand(ctx, api, m.Chat.ID, arg)
	case "/queue":
		d.handleQueueCommand(ctx, api, m.Chat.ID, arg)
	case "/prompt":
		d.handlePromptCommand(ctx, api, m.Chat.ID, arg)
	case "/editprompt":
		d.handleEditPromptCommand(ctx, api, m.Chat.ID, arg)
	case "/cancelprompt":
		d.handleCancelPromptCommand(ctx, api, m.Chat.ID, arg)
	case "/moveprompt":
		d.handleMovePromptCommand(ctx, api, m.Chat.ID, arg)
	case "/flushqueue":
		d.handleFlushQueueCommand(ctx, api, m.Chat.ID, arg)
	case "/peek":
		d.handlePeekCommand(ctx, api, m.Chat.ID, arg)
	case "/interrupt":
		d.handleInterruptCommand(ctx, api, m.Chat.ID, arg)
	case "/kill":
		d.handleKillCommand(ctx, api, m.Chat.ID, arg)
	case "/rename":
		d.handleRenameCommand(ctx, api, m.Chat.ID, arg)
	case "/menu":
		d.handleMenuCommand(ctx, api, m.Chat.ID)
	case "/snooze":
		d.handleSnoozeCommand(ctx, api, m.Chat.ID, arg)
	case "/unsnooze":
		d.handleUnsnoozeCommand(ctx, api, m.Chat.ID, arg)
	case "/log":
		d.handleLogCommand(ctx, api, m.Chat.ID, arg)
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
		return nil, "Multiple active sessions. Use /sessions, then /target <id|name>, /text <id|name>, or /screenshot <id|name>."
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
		cmd := s.Cmd
		if cmd == "" {
			cmd = s.Agent
		}
		fmt.Fprintf(&b, "%s  %s  %s  age=%s  cmd=%s\n", s.ID, s.Name, s.Agent, time.Since(s.StartedAt()).Truncate(time.Second), cmd)
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
	queued := "unknown"
	if d.DB != nil {
		if p, err := d.DB.PromptList(ctx, "", false, 1000); err == nil {
			queued = fmt.Sprintf("%d", len(p))
		}
	}
	return fmt.Sprintf("Onibi status\nuptime=%s\nsessions=%d\npending_approvals=%s\nqueued_prompts=%s\n\n%s",
		time.Since(d.started).Truncate(time.Second), len(d.liveSessions()), pending, queued, d.sessionsText())
}

func helpText() string {
	return strings.Join([]string{
		"Onibi commands:",
		"/sessions - list active sessions",
		"/status - show daemon status",
		"/target <id|name> - set default session",
		"/new <agent> [args...] - start an agent session",
		"/queue [id|name] - list queued prompts",
		"/prompt <text> - queue a prompt",
		"/editprompt <id> <text> - edit a queued prompt",
		"/cancelprompt <id> - cancel a queued prompt",
		"/moveprompt <id> <position> - reorder queued prompts",
		"/flushqueue [id|name] - cancel queued prompts",
		"/peek <id|name> - send session preview",
		"/interrupt <id|name> - send Ctrl-C",
		"/kill <id|name> - terminate session",
		"/rename <id|name> <name> - rename session",
		"/menu - show session actions",
		"/snooze [duration|agent [duration]] - pause non-approval notifications",
		"/unsnooze [agent] - resume notifications",
		"/log [n] - show recent audit entries",
		"/text <id|name> - force text output",
		"/screenshot <id|name> - force screenshots",
		"/help - show this help",
	}, "\n")
}

func (d *Daemon) handleTargetCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	if strings.TrimSpace(arg) == "" {
		id := d.defaultTarget(ctx, chatID)
		if id == "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: d.sessionsText() + "\nNo default target set."})
			return
		}
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Default target: " + id})
		return
	}
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Default target set to " + s.Name + " (" + s.ID + ")."})
}

func (d *Daemon) handleTargetCallback(ctx context.Context, api telegram.API, q *models.CallbackQuery, id string) error {
	chatID := q.From.ID
	s, err := d.sessionByID(id)
	if err != nil {
		answerCallback(ctx, api, q.ID, "Session unavailable")
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Session unavailable."})
		return nil
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	answerCallback(ctx, api, q.ID, "Target set")
	if text := d.popPendingInject(chatID); text != "" {
		return d.injectTelegramText(ctx, api, chatID, s.ID, text)
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Default target set to " + s.Name + " (" + s.ID + ")."})
	return nil
}

func (d *Daemon) handleNewCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /new <agent> [args...]"})
		return
	}
	agent := strings.ToLower(fields[0])
	bin, ok := agentBinary(agent)
	if !ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Unsupported agent. Use: " + strings.Join(supportedAgentNames(), ", ")})
		return
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("%s not found in PATH.", bin)})
		return
	}
	s, err := d.SpawnAgent(ctx, agent, agent, path, fields[1:], nil)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Start failed: " + err.Error()})
		return
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	if api == nil {
		return
	}
	sent, err := api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Started %s (%s). Default target set.", s.Name, s.ID),
	})
	if err == nil {
		d.bindMessage(sent, s.ID)
	}
}

func agentBinary(agent string) (string, bool) {
	defaults := map[string]string{
		"amp":      "amp",
		"claude":   "claude",
		"codex":    "codex",
		"copilot":  "copilot",
		"gemini":   "gemini",
		"goose":    "goose",
		"opencode": "opencode",
		"pi":       "pi",
	}
	bin, ok := defaults[agent]
	if !ok {
		return "", false
	}
	env := "ONIBI_" + strings.ToUpper(agent) + "_BIN"
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v, true
	}
	return bin, true
}

func supportedAgentNames() []string {
	return []string{"amp", "claude", "codex", "copilot", "gemini", "goose", "opencode", "pi"}
}

func (d *Daemon) handleSnoozeCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	scope, dur, err := parseSnooze(arg)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: err.Error()})
		return
	}
	if err := d.setSnooze(ctx, scope, dur); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Snooze failed: " + err.Error()})
		return
	}
	desc := "indefinitely"
	if dur > 0 {
		desc = "for " + dur.String()
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Snoozed " + scope + " " + desc + "."})
}

func (d *Daemon) handleUnsnoozeCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	scope := strings.ToLower(strings.TrimSpace(arg))
	if scope == "" {
		for _, s := range append(supportedAgentNames(), "global", "shell") {
			_ = d.clearSnooze(ctx, s)
		}
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Unsnoozed all notifications."})
		return
	}
	if !validSnoozeScope(scope) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Unknown scope. Use global, shell, or: " + strings.Join(supportedAgentNames(), ", ")})
		return
	}
	_ = d.clearSnooze(ctx, scope)
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Unsnoozed " + scope + "."})
}

func parseSnooze(arg string) (string, time.Duration, error) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(arg)))
	if len(fields) == 0 {
		return "global", time.Hour, nil
	}
	if dur, err := time.ParseDuration(fields[0]); err == nil {
		return "global", dur, nil
	}
	scope := fields[0]
	if !validSnoozeScope(scope) {
		return "", 0, errors.New("Unknown scope. Use global, shell, or: " + strings.Join(supportedAgentNames(), ", "))
	}
	if len(fields) == 1 {
		return scope, 0, nil
	}
	dur, err := time.ParseDuration(fields[1])
	if err != nil {
		return "", 0, errors.New("invalid duration. Example: /snooze 30m or /snooze claude 1h")
	}
	return scope, dur, nil
}

func validSnoozeScope(scope string) bool {
	if scope == "global" || scope == "shell" {
		return true
	}
	for _, s := range supportedAgentNames() {
		if scope == s {
			return true
		}
	}
	return false
}

func (d *Daemon) setSnooze(ctx context.Context, scope string, dur time.Duration) error {
	if d.DB == nil {
		return nil
	}
	expire := int64(0)
	value := "indefinite"
	if dur > 0 {
		expire = time.Now().Add(dur).Unix()
		value = time.Unix(expire, 0).Format(time.RFC3339)
	}
	if err := d.DB.KVSet(ctx, snoozeKey(scope), []byte(value), expire); err != nil {
		return err
	}
	_ = d.DB.AuditAppend(ctx, "notify.snooze", "", scope+" "+value, 0, scope)
	return nil
}

func (d *Daemon) clearSnooze(ctx context.Context, scope string) error {
	if d.DB == nil {
		return nil
	}
	if err := d.DB.KVDel(ctx, snoozeKey(scope)); err != nil {
		return err
	}
	_ = d.DB.AuditAppend(ctx, "notify.unsnooze", "", scope, 0, scope)
	return nil
}

func (d *Daemon) isSnoozed(ctx context.Context, agent string) bool {
	if d.DB == nil {
		return false
	}
	if _, ok, _ := d.DB.KVGet(ctx, snoozeKey("global")); ok {
		return true
	}
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "" {
		return false
	}
	_, ok, _ := d.DB.KVGet(ctx, snoozeKey(agent))
	return ok
}

func snoozeKey(scope string) string {
	return "snooze:" + strings.ToLower(strings.TrimSpace(scope))
}

func (d *Daemon) handleLogCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	if d.DB == nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "No audit DB."})
		return
	}
	n := 10
	if strings.TrimSpace(arg) != "" {
		if _, err := fmt.Sscanf(strings.TrimSpace(arg), "%d", &n); err != nil || n <= 0 {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /log [n]"})
			return
		}
	}
	entries, err := d.DB.AuditRecent(ctx, n)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Log read failed: " + err.Error()})
		return
	}
	if len(entries) == 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Audit log empty."})
		return
	}
	var b strings.Builder
	b.WriteString("Recent audit:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "%s %s %s %s\n", e.TS.Format("15:04:05"), e.Action, shortID(e.SessionID), e.Detail)
		if b.Len() > 3800 {
			b.WriteString("...")
			break
		}
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: strings.TrimRight(b.String(), "\n")})
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
