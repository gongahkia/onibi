package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

func (d *Daemon) handleTextCommand(ctx context.Context, api telegram.API, m *models.Message) bool {
	cmd, arg, ok := parseTelegramCommand(m.Text)
	if !ok {
		return false
	}
	switch cmd {
	case "/start":
		d.handleStartCommand(ctx, api, m.Chat.ID, arg)
	case "/ping":
		_, _ = d.sendMaybeEncryptedText(ctx, api, m.Chat.ID, "ping", "Onibi ping", d.pingText(ctx, telegramIngressLag(m)))
	case "/sessions":
		_, _ = d.sendMaybeEncryptedText(ctx, api, m.Chat.ID, "sessions", "Active sessions", d.sessionsText(ctx, m.Chat.ID))
	case "/status":
		_, _ = d.sendMaybeEncryptedText(ctx, api, m.Chat.ID, "status", "Onibi status", d.statusText(ctx, m.Chat.ID))
	case "/help":
		if strings.TrimSpace(arg) == "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: telegram.HelpText()})
		} else {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: telegram.HelpDetail(arg)})
		}
	case "/secure":
		if strings.EqualFold(strings.TrimSpace(arg), "status") {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: d.secureStatusText(ctx)})
		} else {
			d.sendSecureRequired(ctx, api, m.Chat.ID)
		}
	case "/text":
		d.handleRenderOverride(ctx, api, m.Chat.ID, arg, render.ModeText)
	case "/render":
		d.handleRenderOverride(ctx, api, m.Chat.ID, arg, render.ModePNG)
	case "/screenshot":
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Using /render. This is a terminal-buffer render, not a Ghostty window screenshot.",
		})
		d.handleRenderOverride(ctx, api, m.Chat.ID, arg, render.ModePNG)
	case "/target":
		d.handleTargetCommand(ctx, api, m.Chat.ID, arg)
	case "/new":
		d.handleNewCommand(ctx, api, m.Chat.ID, arg)
	case "/project":
		d.handleProjectCommand(ctx, api, m.Chat.ID, arg)
	case "/show":
		d.handleShowCommand(ctx, api, m.Chat.ID, arg)
	case "/hide":
		d.handleHideCommand(ctx, api, m.Chat.ID, arg)
	case "/queue":
		d.handleQueueCommand(ctx, api, m.Chat.ID, arg)
	case "/prompt":
		if d.encryptedModeEnabled() {
			d.sendSecureBlocked(ctx, api, m.Chat.ID)
			return true
		}
		d.handlePromptCommand(ctx, api, m.Chat.ID, arg)
	case "/send":
		if d.encryptedModeEnabled() {
			d.sendSecureBlocked(ctx, api, m.Chat.ID)
			return true
		}
		d.handleSendCommand(ctx, api, m.Chat.ID, arg)
	case "/editprompt":
		if d.encryptedModeEnabled() {
			d.sendSecureBlocked(ctx, api, m.Chat.ID)
			return true
		}
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
	case "/enter":
		d.handleEnterCommand(ctx, api, m.Chat.ID, arg)
	case "/esc":
		d.handleEscCommand(ctx, api, m.Chat.ID, arg)
	case "/kill":
		d.handleKillCommand(ctx, api, m.Chat.ID, arg)
	case "/rename":
		if d.encryptedModeEnabled() {
			d.sendSecureBlocked(ctx, api, m.Chat.ID)
			return true
		}
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

func (d *Daemon) handleStartCommand(ctx context.Context, api telegram.API, chatID int64, _ string) {
	text := "Onibi is paired and listening.\n\nChoose a guided action below, or send /menu anytime."
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: telegram.OnboardingKeyboard()})
}

func (d *Daemon) handleRenderOverride(ctx context.Context, api telegram.API, chatID int64, target string, mode render.Mode) {
	s, msg := d.resolveSessionTarget(target)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	d.setRenderOverride(s.ID, mode)
	if mode == render.ModePNG {
		d.sendSessionPreview(ctx, api, chatID, s)
		return
	}
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
		return nil, "Multiple active sessions. Use /sessions, then /target <id|name>, /text <id|name>, or /render <id|name>."
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

func (d *Daemon) sessionsText(ctx context.Context, chatID int64) string {
	live := d.liveSessions()
	if len(live) == 0 {
		d.clearStaleDefaultTarget(ctx, chatID)
		return "No active sessions.\nNext: /new shell, /new claude, or open tmux on the laptop and send /new tmux <target>."
	}
	defaultID := d.activeDefaultTarget(ctx, chatID)
	var b strings.Builder
	b.WriteString("Active sessions:\n")
	b.WriteString(d.sessionCardsText(ctx, chatID, live))
	if defaultID == "" {
		if len(live) == 1 {
			b.WriteString("\n* = default target (implicit)")
		} else {
			b.WriteString("\n* = default target (none set)")
		}
	} else {
		b.WriteString("\n* = default target")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (d *Daemon) statusText(ctx context.Context, chatID int64) string {
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
	return fmt.Sprintf("Onibi status\nuptime=%s\nencrypted_mode=%s\ndefault_target=%s\ntelegram_poller=%s\nsnooze=%s\nsessions=%d\npending_approvals=%s\nqueued_prompts=%s\n\n%s",
		time.Since(d.started).Truncate(time.Second), d.encryptedModeLabel(), d.defaultTargetLabel(ctx, chatID), d.telegramPollerStatus(ctx), d.snoozeStatus(ctx), len(d.liveSessions()), pending, queued, d.sessionsText(ctx, chatID))
}

func (d *Daemon) menuText(ctx context.Context, chatID int64) string {
	live := d.liveSessions()
	total, busy, headless, visible := d.menuSessionCounts(ctx, live)
	text := fmt.Sprintf("Onibi\ndaemon: %s\ntarget: %s\nsessions: %d total, %d busy, %d headless, %d visible\napprovals: %s pending\nqueue: %s queued\nsnooze: %s\nsecure: %s\nhooks: %s",
		d.menuDaemonState(ctx), d.menuTargetLabel(ctx, chatID, live), total, busy, headless, visible, d.pendingApprovalCount(ctx), d.queuedPromptCount(ctx), d.menuSnoozeLabel(ctx), d.secureStatus(ctx), d.hookHealthSummary(ctx))
	if len(live) > 0 {
		text += "\n\n" + d.sessionCardsText(ctx, chatID, live)
	}
	return text
}

func (d *Daemon) menuDaemonState(ctx context.Context) string {
	if d.DB == nil || d.Queue == nil {
		return "degraded"
	}
	if d.telegramPollerStatus(ctx) != "ok" {
		return "degraded"
	}
	return "up"
}

func (d *Daemon) menuTargetLabel(ctx context.Context, chatID int64, live []*Session) string {
	id := d.activeDefaultTarget(ctx, chatID)
	if id == "" && len(live) == 1 {
		return live[0].Name + " (" + shortID(live[0].ID) + ")"
	}
	if id == "" {
		return "none"
	}
	if s, err := d.sessionByID(id); err == nil {
		return s.Name + " (" + shortID(s.ID) + ")"
	}
	return "none"
}

func (d *Daemon) menuSessionCounts(ctx context.Context, live []*Session) (total, busy, headless, visible int) {
	for _, s := range live {
		total++
		if d.sessionState(s) == "busy" {
			busy++
		}
		if strings.HasPrefix(d.sessionMode(ctx, s), "visible") {
			visible++
			continue
		}
		headless++
	}
	return total, busy, headless, visible
}

func (d *Daemon) pendingApprovalCount(ctx context.Context) string {
	if d.DB == nil || d.Queue == nil {
		return "unknown"
	}
	pending, err := d.Queue.Pending(ctx)
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", len(pending))
}

func (d *Daemon) queuedPromptCount(ctx context.Context) string {
	if d.DB == nil {
		return "unknown"
	}
	rows, err := d.DB.PromptList(ctx, "", false, 1000)
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", len(rows))
}

func (d *Daemon) menuSnoozeLabel(ctx context.Context) string {
	status := d.snoozeStatus(ctx)
	switch {
	case status == "none":
		return "off"
	case status == "unknown":
		return "unknown"
	case strings.Contains(status, "global="):
		return "global"
	default:
		return "agent"
	}
}

func (d *Daemon) hookHealthSummary(ctx context.Context) string {
	if d.DB == nil {
		return "unknown"
	}
	rows, err := d.DB.SQL().QueryContext(ctx, `SELECT agent FROM hooks ORDER BY agent`)
	if err != nil {
		return "fail"
	}
	defer rows.Close()
	seen := map[string]bool{}
	total, warns, fails := 0, 0, 0
	for rows.Next() {
		var agent string
		if err := rows.Scan(&agent); err != nil {
			return "fail"
		}
		if seen[agent] {
			continue
		}
		seen[agent] = true
		total++
		info, ok := d.hookInfo(ctx, agent)
		if !ok {
			warns++
			continue
		}
		switch hookHealthLevel(info) {
		case "fail":
			fails++
		case "warn":
			warns++
		}
	}
	if err := rows.Err(); err != nil {
		return "fail"
	}
	switch {
	case total == 0:
		return "warn"
	case fails > 0:
		return fmt.Sprintf("fail (%d)", fails)
	case warns > 0:
		return fmt.Sprintf("warn (%d)", warns)
	default:
		return "ok"
	}
}

func (d *Daemon) hookInfo(ctx context.Context, agent string) (common.Info, bool) {
	if strings.HasPrefix(agent, "shell:") {
		return adapters.ShellStatus(ctx, d.DB, strings.TrimPrefix(agent, "shell:")), true
	}
	if a, ok := adapters.Get(agent); ok {
		return a.Status(ctx, d.DB), true
	}
	return common.Info{}, false
}

func hookHealthLevel(info common.Info) string {
	switch {
	case !info.Installed || !info.Managed || !info.HashRecorded || info.Tampered:
		return "fail"
	case info.Outdated || info.Adoptable:
		return "warn"
	default:
		return "ok"
	}
}

func (d *Daemon) menuTargets(ctx context.Context, chatID int64, live []*Session) []telegram.SessionTarget {
	defaultID := d.activeDefaultTarget(ctx, chatID)
	actionID := defaultID
	if actionID == "" && len(live) == 1 {
		actionID = live[0].ID
	}
	targets := make([]telegram.SessionTarget, 0, len(live))
	for _, s := range live {
		mode := d.sessionMode(ctx, s)
		targets = append(targets, telegram.SessionTarget{
			ID:       s.ID,
			Label:    s.Name + " " + s.Agent + " " + shortID(s.ID),
			Selected: s.ID == actionID,
			Visible:  strings.HasPrefix(mode, "visible"),
		})
	}
	return targets
}

func (d *Daemon) pingText(ctx context.Context, ingressLag time.Duration) string {
	parts := []string{
		"pong",
		"uptime=" + time.Since(d.started).Truncate(time.Second).String(),
		fmt.Sprintf("sessions=%d", len(d.liveSessions())),
		"telegram_poller=" + d.telegramPollerStatus(ctx),
	}
	if ingressLag >= 0 {
		parts = append(parts, "telegram_ingress_lag="+ingressLag.Truncate(time.Second).String())
	}
	return strings.Join(parts, "\n")
}

func telegramIngressLag(m *models.Message) time.Duration {
	if m == nil || m.Date <= 0 {
		return -1
	}
	lag := time.Since(time.Unix(int64(m.Date), 0))
	if lag < 0 {
		return 0
	}
	return lag
}

func (d *Daemon) sessionState(s *Session) string {
	if s == nil {
		return "unknown"
	}
	if s.Ended() {
		return "ended"
	}
	d.threadMu.RLock()
	busy := d.busySessions[s.ID]
	d.threadMu.RUnlock()
	if busy {
		return "busy"
	}
	return "idle"
}

func (d *Daemon) sessionMode(ctx context.Context, s *Session) string {
	if s == nil {
		return "unknown"
	}
	if s.Ended() {
		return "ended"
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return "legacy pty"
	}
	n, err := newTmuxController().AttachCount(ctx, s.TmuxTarget)
	if err != nil || n == 0 {
		return "headless"
	}
	if n == 1 {
		return "visible"
	}
	return fmt.Sprintf("visible x%d", n)
}

func (d *Daemon) encryptedModeLabel() string {
	mode := strings.ToLower(strings.TrimSpace(d.EncryptedMode))
	if mode == "" {
		return "off"
	}
	return mode
}

func (d *Daemon) telegramPollerStatus(ctx context.Context) string {
	if d.DB == nil {
		return "unknown"
	}
	detail, ok, err := d.DB.KVGetString(ctx, store.TelegramPollerConflictKey)
	if err != nil {
		return "unknown"
	}
	if ok && strings.TrimSpace(detail) != "" {
		detail = strings.TrimSpace(detail)
		if strings.HasPrefix(detail, "conflict:") {
			return detail
		}
		return "conflict: " + detail
	}
	return "ok"
}

func (d *Daemon) defaultTargetLabel(ctx context.Context, chatID int64) string {
	id := d.activeDefaultTarget(ctx, chatID)
	if id == "" {
		return "none"
	}
	if s, err := d.sessionByID(id); err == nil {
		return s.Name + " (" + shortID(s.ID) + ")"
	}
	return "none"
}

func (d *Daemon) activeDefaultTarget(ctx context.Context, chatID int64) string {
	id := d.defaultTarget(ctx, chatID)
	if id == "" {
		return ""
	}
	if _, err := d.sessionByID(id); err == nil {
		return id
	}
	d.clearDefaultTarget(ctx, chatID)
	return ""
}

func (d *Daemon) clearStaleDefaultTarget(ctx context.Context, chatID int64) {
	if d.defaultTarget(ctx, chatID) != "" {
		d.clearDefaultTarget(ctx, chatID)
	}
}

func (d *Daemon) snoozeStatus(ctx context.Context) string {
	if d.DB == nil {
		return "unknown"
	}
	scopes := append([]string{"global", "shell"}, supportedAgentNames()...)
	var active []string
	for _, scope := range scopes {
		v, ok, err := d.DB.KVGetString(ctx, snoozeKey(scope))
		if err == nil && ok {
			active = append(active, scope+"="+v)
		}
	}
	if len(active) == 0 {
		return "none"
	}
	return strings.Join(active, ", ")
}

func (d *Daemon) handleSendCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	if strings.TrimSpace(arg) == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /send <text>"})
		return
	}
	_ = d.sendImmediateText(ctx, api, chatID, "", arg)
}

func (d *Daemon) handleEnterCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	d.handlePTYByteCommand(ctx, api, chatID, arg, "\n", "Enter", "/enter", "telegram.enter")
}

func (d *Daemon) handleEscCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	d.handlePTYByteCommand(ctx, api, chatID, arg, "\x1b", "Esc", "/esc", "telegram.esc")
}

func (d *Daemon) handlePTYByteCommand(ctx context.Context, api telegram.API, chatID int64, arg, payload, label, usage, anomaly string) {
	s, err := d.resolveInjectTarget(ctx, chatID, strings.TrimSpace(arg))
	if errors.Is(err, errAmbiguousTarget) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Multiple active sessions. Use " + usage + " <id|name>."})
		return
	}
	if errors.Is(err, ErrUnknownSession) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "No active PTY session."})
		return
	}
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: label + " failed: " + err.Error()})
		return
	}
	if s.Host == nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Session has no writable PTY."})
		return
	}
	if _, err := s.Host.Write([]byte(payload)); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: label + " failed: " + err.Error()})
		return
	}
	d.touchSession(ctx, s)
	d.noteAnomaly(ctx, anomaly)
	d.setDefaultTarget(ctx, chatID, s.ID)
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "prompt", label+" sent", "Sent "+label+" to "+s.Name+" ("+s.ID+").")
}

func (d *Daemon) handleTargetCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	if strings.TrimSpace(arg) == "" {
		id := d.defaultTarget(ctx, chatID)
		if id == "" {
			_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "target", "Target", d.sessionsText(ctx, chatID)+"\nNo default target set.")
			return
		}
		_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "target", "Target", "Default target: "+id)
		return
	}
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "target", "Target", "Default target set to "+s.Name+" ("+s.ID+").")
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
	if text := d.popPendingSend(ctx, chatID); text != "" {
		return d.sendImmediateText(ctx, api, chatID, s.ID, text)
	}
	if text := d.popPendingInject(ctx, chatID); text != "" {
		return d.injectTelegramText(ctx, api, chatID, s.ID, text)
	}
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "target", "Target", "Default target set to "+s.Name+" ("+s.ID+").")
	return nil
}

func (d *Daemon) handleNewCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	fields, parseErr := splitCommandFields(arg)
	if parseErr != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Parse failed: " + parseErr.Error()})
		return
	}
	if len(fields) == 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /new [--headless|--visible] <agent|shell|tmux> (--project <alias>|--cwd <path>) [args...]"})
		return
	}
	mode := "headless"
	cwd := ""
	project := ""
	for len(fields) > 0 {
		switch normalizeOptionToken(fields[0]) {
		case "--headless", "headless":
			mode = "headless"
			fields = fields[1:]
		case "--visible", "visible":
			mode = "visible"
			fields = fields[1:]
		case "--cwd":
			if len(fields) < 2 {
				sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /new --cwd <path> <agent>"})
				return
			}
			cwd = fields[1]
			fields = fields[2:]
		case "--project", "--repo":
			if len(fields) < 2 {
				sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /new --project <alias> <agent>"})
				return
			}
			project = fields[1]
			fields = fields[2:]
		default:
			goto parsedMode
		}
	}
parsedMode:
	if len(fields) == 0 {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /new [--headless|--visible] <agent|shell|tmux> (--project <alias>|--cwd <path>) [args...]"})
		return
	}
	agent := strings.ToLower(fields[0])
	if agent == "tmux" {
		d.handleNewTmuxCommand(ctx, api, chatID, fields[1:])
		return
	}
	cwd, err := d.resolveNewSessionCWD(ctx, project, cwd)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: err.Error()})
		return
	}
	if cwd == "" {
		d.sendProjectRequired(ctx, api, chatID, fields, mode)
		return
	}
	bin, spawnAgent, spawnArgs, ok := agentCommand(agent, fields[1:])
	if !ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Unsupported target. Use shell, tmux, or: " + strings.Join(supportedAgentNames(), ", ")})
		return
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("%s not found in PATH.", bin)})
		return
	}
	s, err := d.StartTmuxSession(ctx, spawnAgent, spawnAgent, path, spawnArgs, cwd)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Start failed: " + err.Error()})
		return
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	detail := fmt.Sprintf("Started %s (%s) headless in %s. Default target set.", s.Name, s.ID, cwd)
	if mode == "visible" {
		if msg, err := d.ShowSession(ctx, s.ID); err == nil {
			detail = fmt.Sprintf("Started %s (%s) visible in %s. %s Default target set.", s.Name, s.ID, cwd, msg)
		} else {
			detail = fmt.Sprintf("Started %s (%s) headless. Show failed: %s", s.Name, s.ID, err.Error())
		}
	}
	if api == nil {
		return
	}
	if d.encryptedModeEnabled() {
		sent, err := d.sendEncryptedText(ctx, api, chatID, "new", "Started session", detail)
		if err == nil {
			d.bindMessage(sent, s.ID)
		} else {
			d.sendSecureRequired(ctx, api, chatID)
		}
		return
	}
	sent, err := api.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   detail,
	})
	if err == nil {
		d.bindMessage(sent, s.ID)
	}
}

func (d *Daemon) handleProjectCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	fields, err := splitCommandFields(arg)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Parse failed: " + err.Error()})
		return
	}
	if len(fields) == 0 || fields[0] == "list" {
		d.sendProjectList(ctx, api, chatID)
		return
	}
	switch fields[0] {
	case "add":
		if len(fields) < 3 {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /project add <alias> <path>"})
			return
		}
		alias := sanitizeProjectAlias(fields[1])
		if alias == "" {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Project alias must contain letters, numbers, dot, underscore, or dash."})
			return
		}
		path, err := normalizeProjectPath(strings.Join(fields[2:], " "))
		if err != nil {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: err.Error()})
			return
		}
		if d.DB == nil {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Project DB unavailable."})
			return
		}
		if err := d.DB.KVSetString(ctx, projectAliasKey(alias), path); err != nil {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Project save failed: " + err.Error()})
			return
		}
		d.audit(ctx, "project.add", "", path, chatID, "alias="+alias)
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Project %s saved: %s", alias, path)})
	case "forget", "del", "delete", "remove":
		if len(fields) != 2 {
			sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /project forget <alias>"})
			return
		}
		alias := sanitizeProjectAlias(fields[1])
		if d.DB != nil {
			_ = d.DB.KVDel(ctx, projectAliasKey(alias))
		}
		d.audit(ctx, "project.forget", "", "", chatID, "alias="+alias)
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Project forgotten: " + alias})
	default:
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /project list | /project add <alias> <path> | /project forget <alias>"})
	}
}

func normalizeOptionToken(s string) string {
	runes := []rune(s)
	if len(runes) > 0 && isSmartDash(runes[0]) {
		i := 0
		for i < len(runes) && isSmartDash(runes[i]) {
			i++
		}
		return "--" + normalizeDashes(string(runes[i:]))
	}
	return normalizeDashes(s)
}

func normalizeDashes(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '‐', '‑', '‒', '–', '—', '―', '−':
			return '-'
		default:
			return r
		}
	}, s)
}

func isSmartDash(r rune) bool {
	switch r {
	case '‐', '‑', '‒', '–', '—', '―', '−':
		return true
	default:
		return false
	}
}

func (d *Daemon) resolveNewSessionCWD(ctx context.Context, project, cwd string) (string, error) {
	if strings.TrimSpace(project) != "" && strings.TrimSpace(cwd) != "" {
		return "", errors.New("Use either --project or --cwd, not both.")
	}
	if project != "" {
		if d.DB == nil {
			return "", errors.New("Project DB unavailable.")
		}
		path, ok, err := d.DB.KVGetString(ctx, projectAliasKey(sanitizeProjectAlias(project)))
		if err != nil {
			return "", err
		}
		if !ok || path == "" {
			return "", errors.New("Unknown project alias. Use /project list or /project add <alias> <path>.")
		}
		normalized, err := normalizeProjectPath(path)
		if err != nil {
			alias := sanitizeProjectAlias(project)
			return "", fmt.Errorf("Project %s path is invalid: %w. Repair: /project add %s <path> or /project forget %s", alias, err, alias, alias)
		}
		return normalized, nil
	}
	if strings.TrimSpace(cwd) != "" {
		return normalizeProjectPath(cwd)
	}
	return "", nil
}

func (d *Daemon) sendProjectRequired(ctx context.Context, api telegram.API, chatID int64, fields []string, mode string) {
	var b strings.Builder
	b.WriteString("Choose a project before starting this session.\n\n")
	b.WriteString("Use /new --")
	b.WriteString(mode)
	b.WriteString(" --project <alias> ")
	b.WriteString(strings.Join(fields, " "))
	b.WriteString("\nor /new --")
	b.WriteString(mode)
	b.WriteString(" --cwd <path> ")
	b.WriteString(strings.Join(fields, " "))
	b.WriteString("\n\n")
	b.WriteString(d.projectListText(ctx))
	params := &tgbot.SendMessageParams{ChatID: chatID, Text: b.String()}
	if aliases, err := d.projectAliases(ctx); err == nil && len(aliases) > 0 {
		params.ReplyMarkup = telegram.ProjectAliasKeyboard(aliases)
	}
	sendMessage(ctx, api, params)
}

func (d *Daemon) sendProjectList(ctx context.Context, api telegram.API, chatID int64) {
	params := &tgbot.SendMessageParams{ChatID: chatID, Text: d.projectListText(ctx)}
	if aliases, err := d.projectAliases(ctx); err == nil && len(aliases) > 0 {
		params.ReplyMarkup = telegram.ProjectAliasKeyboard(aliases)
	}
	sendMessage(ctx, api, params)
}

func (d *Daemon) projectListText(ctx context.Context) string {
	if d.DB == nil {
		return "Projects unavailable."
	}
	keys, err := d.projectAliasKeys(ctx)
	if err != nil {
		return "Project read failed: " + err.Error()
	}
	if len(keys) == 0 {
		return "No project aliases. Add one with /project add <alias> <path>."
	}
	var b strings.Builder
	b.WriteString("Projects:")
	for _, key := range keys {
		alias := strings.TrimPrefix(key, projectAliasPrefix)
		path, ok, _ := d.DB.KVGetString(ctx, key)
		if ok {
			fmt.Fprintf(&b, "\n%s  %s", alias, projectHealth(path))
		}
	}
	return b.String()
}

func (d *Daemon) firstProjectAlias(ctx context.Context) string {
	aliases, err := d.projectAliases(ctx)
	if err != nil || len(aliases) == 0 {
		return ""
	}
	return aliases[0]
}

func (d *Daemon) projectAliases(ctx context.Context) ([]string, error) {
	keys, err := d.projectAliasKeys(ctx)
	if err != nil {
		return nil, err
	}
	aliases := make([]string, 0, len(keys))
	for _, key := range keys {
		aliases = append(aliases, strings.TrimPrefix(key, projectAliasPrefix))
	}
	return aliases, nil
}

func (d *Daemon) projectAliasKeys(ctx context.Context) ([]string, error) {
	if d.DB == nil {
		return nil, errors.New("Project DB unavailable.")
	}
	return d.DB.KVKeysWithPrefix(ctx, projectAliasPrefix)
}

const projectAliasPrefix = "project_alias:"

func projectAliasKey(alias string) string {
	return projectAliasPrefix + alias
}

func sanitizeProjectAlias(alias string) string {
	alias = strings.ToLower(strings.TrimSpace(alias))
	var b strings.Builder
	for _, r := range alias {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	runes := []rune(b.String())
	if len(runes) > 32 {
		return string(runes[:32])
	}
	return string(runes)
}

func normalizeProjectPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("project path required")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project path unavailable: %w", err)
	}
	if !st.IsDir() {
		return "", errors.New("project path is not a directory")
	}
	return abs, nil
}

func projectHealth(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "missing repair=/project add"
	}
	if !st.IsDir() {
		return "not-dir repair=/project add"
	}
	write := "read-only"
	if st.Mode().Perm()&0o200 != 0 {
		write = "writable"
	}
	git := "no-git"
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		git = "git"
	}
	return "ok " + write + " " + git
}

func splitCommandFields(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return out, nil
}

func (d *Daemon) handleShowCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	text, err := d.ShowSession(ctx, s.ID)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Show failed: " + err.Error()})
		return
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: text})
}

func (d *Daemon) handleHideCommand(ctx context.Context, api telegram.API, chatID int64, arg string) {
	s, msg := d.resolveSessionTarget(arg)
	if msg != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
		return
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        "Hide " + s.Name + " (" + s.ID + ")?",
		ReplyMarkup: telegram.HideChoiceKeyboard(s.ID),
	})
}

func (d *Daemon) handleNewTmuxCommand(ctx context.Context, api telegram.API, chatID int64, args []string) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /new tmux <target>\nExample: /new tmux onibi or /new tmux %1"})
		return
	}
	target := strings.TrimSpace(strings.Join(args, " "))
	s, err := d.AttachTmux(ctx, "tmux:"+target, target)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: chatID, Text: "Attach failed: " + err.Error()})
		return
	}
	d.setDefaultTarget(ctx, chatID, s.ID)
	sent, err := d.sendMaybeEncryptedText(ctx, api, chatID, "new", "Attached tmux", fmt.Sprintf("Attached tmux %s as %s (%s). Default target set.", target, s.Name, s.ID))
	if err == nil {
		d.bindMessage(sent, s.ID)
	}
}

func agentCommand(agent string, args []string) (string, string, []string, bool) {
	if agent == "shell" {
		shell := defaultInteractiveShell()
		if len(args) > 0 {
			shell = args[0]
			args = args[1:]
		}
		bin, shellArgs, ok := shellCommand(shell, args)
		return bin, "shell", shellArgs, ok
	}
	bin, ok := agentBinary(agent)
	return bin, agent, args, ok
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

func defaultInteractiveShell() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		return filepath.Base(sh)
	}
	if runtime.GOOS == "darwin" {
		return "zsh"
	}
	return "bash"
}

func shellCommand(shell string, extra []string) (string, []string, bool) {
	shell = strings.ToLower(strings.TrimSpace(filepath.Base(shell)))
	var args []string
	switch shell {
	case "zsh":
		args = []string{"-i"}
	case "bash":
		args = []string{"-i"}
	case "fish":
		args = []string{"--interactive"}
	case "sh", "dash", "ash":
		args = []string{"-i"}
	case "nu", "pwsh", "powershell", "ksh", "ksh93", "mksh", "oksh", "tcsh", "csh":
	default:
		return "", nil, false
	}
	return shell, append(args, extra...), true
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
	d.audit(ctx, "notify.snooze", "", scope+" "+value, 0, scope)
	return nil
}

func (d *Daemon) clearSnooze(ctx context.Context, scope string) error {
	if d.DB == nil {
		return nil
	}
	if err := d.DB.KVDel(ctx, snoozeKey(scope)); err != nil {
		return err
	}
	d.audit(ctx, "notify.unsnooze", "", scope, 0, scope)
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
	_, _ = d.sendMaybeEncryptedText(ctx, api, chatID, "audit", "Recent audit", strings.TrimRight(b.String(), "\n"))
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
