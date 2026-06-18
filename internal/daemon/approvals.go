package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/telegram"
)

// handleApprovalRequest is the RPC handler invoked when an onibi-notify
// --wait client sends an approval_request. It:
//  1. creates an approval row, gets the in-memory waiter channel
//  2. renders the tool call + scrubbed inputs to Telegram with [Approve]
//     [Deny][Edit] keyboard
//  3. records (chat, msg) on the approval row so the callback handler can
//     edit the message in place when the decision lands
//  4. blocks reading the waiter channel until decision or ctx cancel
//  5. returns the intake.Response (the server writes it back to the
//     blocked hook, which formats Claude's expected JSON and exits)
func (d *Daemon) handleApprovalRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	s, reason := d.sessionForEvent(ev)
	if s == nil {
		d.auditIgnoredHook(ctx, "approval.ignored", ev, reason)
		return intake.Response{Decision: "cancelled", Reason: "unmanaged or unknown Onibi session"}, nil
	}
	ev.Session = s.ID
	d.appendEventOutput(s, ev)

	// fall back to session label if known (for nicer message header)
	sessLabel := s.Name + " (" + s.ID + ")"

	approvalID, ch, err := d.Queue.Request(ctx, ev.Session, ev.Agent, ev.Tool, ev.InputJSON)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}

	a, getErr := d.Queue.Get(ctx, approvalID)
	if getErr != nil {
		return intake.Response{Decision: "cancelled", Reason: getErr.Error()}, nil
	}
	d.noteAnomaly(ctx, "approval.request")
	if isHighRiskApproval(a) {
		d.noteAnomaly(ctx, "approval.high_risk")
	}
	renderCtx := approvalRenderContext{
		Agent:        ev.Agent,
		SessionLabel: sessLabel,
		CWD:          ev.CWD,
		ToolTarget:   ev.ToolTarget,
		Command:      ev.Command,
		FilePath:     ev.FilePath,
		ExpiresAt:    a.ExpiresAt,
	}
	sent, sendErr := d.sendApprovalMessageWithContext(ctx, approvalID, ev.Tool, ev.InputJSON, false, renderCtx)
	if sendErr != nil {
		// Telegram unreachable — cancel the approval so the hook unblocks
		_ = d.Queue.Cancel(context.Background(), approvalID, "telegram send failed: "+sendErr.Error())
		<-ch // drain
		return intake.Response{Decision: "cancelled", Reason: "telegram send failed"}, nil
	}
	if sent != nil {
		_ = d.Queue.SetMessage(ctx, approvalID, sent.Chat.ID, int64(sent.ID))
	}

	// audit: request raised
	d.audit(ctx, "approval.request", ev.Session, ev.InputJSON, 0,
		fmt.Sprintf("tool=%s id=%s", ev.Tool, approvalID))

	// wait for decision
	select {
	case dec := <-ch:
		return d.respondAndAnnotate(ctx, approvalID, sent, dec, ev)
	case <-ctx.Done():
		// server is shutting down; cancel the approval so the hook unblocks
		_ = d.Queue.Cancel(context.Background(), approvalID, "daemon shutdown")
		return intake.Response{Decision: "cancelled", Reason: "daemon shutdown"}, nil
	}
}

// RestorePendingApprovals re-renders pending approvals after a daemon restart.
// It does not recreate waiters: the original hook may already have failed
// open. The row remains useful for owner visibility and audit.
func (d *Daemon) RestorePendingApprovals(ctx context.Context) error {
	if d.Queue == nil || d.Bot == nil || d.Owner == nil {
		return nil
	}
	pending, err := d.Queue.Pending(ctx)
	if err != nil {
		return err
	}
	for _, a := range pending {
		sessLabel := a.SessionID
		if s, err := d.Registry.Get(a.SessionID); err == nil {
			sessLabel = s.Name + " (" + s.ID + ")"
		}
		if a.ChatID != 0 && a.MsgID != 0 {
			edited, fallback := d.tryEditApprovalInPlace(ctx, a, sessLabel)
			if edited {
				_ = d.Queue.SetMessage(ctx, a.ID, a.ChatID, a.MsgID)
				continue
			}
			if !fallback {
				continue
			}
		}
		sent, err := d.sendApprovalMessageWithContext(ctx, a.ID, a.Tool, a.InputJSON, true, approvalRenderContext{
			Agent:        a.Agent,
			SessionLabel: sessLabel,
			ExpiresAt:    a.ExpiresAt,
		})
		if err != nil {
			d.Log.Warn("restore approval message", slog.String("id", a.ID), slog.Any("err", err))
			continue
		}
		if sent != nil {
			_ = d.Queue.SetMessage(ctx, a.ID, sent.Chat.ID, int64(sent.ID))
		}
	}
	return nil
}

func (d *Daemon) tryEditApprovalInPlace(ctx context.Context, a *approval.Approval, sessLabel string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(d.EncryptedMode)) {
	case "on", "ask":
		return false, true
	}
	body := renderApproval(a.Tool, a.InputJSON, approvalRenderContext{Agent: a.Agent, SessionLabel: sessLabel, ExpiresAt: a.ExpiresAt}).HTML
	_, err := d.Bot.EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID:      a.ChatID,
		MessageID:   int(a.MsgID),
		Text:        body,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: telegram.ApprovalKeyboard(a.ID),
	})
	if err != nil {
		d.Log.Info("restore approval edit-in-place failed", slog.String("id", a.ID), slog.Any("err", err))
		return false, shouldFallbackApprovalEdit(err)
	}
	telegram.MarkAwaitingOwnerInteraction(d.Bot, a.ChatID)
	d.bindMessage(&models.Message{ID: int(a.MsgID), Chat: models.Chat{ID: a.ChatID}}, a.SessionID)
	return true, false
}

func shouldFallbackApprovalEdit(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "message to edit not found")
}

func (d *Daemon) sendApprovalMessage(ctx context.Context, id, tool, inputJSON, sessLabel string, restored bool, expires time.Time) (*models.Message, error) {
	return d.sendApprovalMessageWithContext(ctx, id, tool, inputJSON, restored, approvalRenderContext{SessionLabel: sessLabel, ExpiresAt: expires})
}

func (d *Daemon) sendApprovalMessageWithContext(ctx context.Context, id, tool, inputJSON string, restored bool, renderCtx approvalRenderContext) (*models.Message, error) {
	if d.Bot == nil {
		return nil, errors.New("telegram bot unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(d.EncryptedMode)) {
	case "on", "ask":
		return d.sendEncryptedApprovalMessage(ctx, id, tool, inputJSON, restored, renderCtx)
	default:
		return d.sendPlainApprovalMessage(ctx, id, tool, inputJSON, restored, renderCtx)
	}
}

func (d *Daemon) sendPlainApprovalMessage(ctx context.Context, id, tool, inputJSON string, restored bool, renderCtx approvalRenderContext) (*models.Message, error) {
	rendered := renderApproval(tool, inputJSON, renderCtx)
	msg := rendered.HTML
	if restored {
		msg += "\n\nRe-sent after daemon restart. The original hook may have already proceeded."
	}
	sent, err := d.Bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      d.Owner.ID(),
		Text:        msg,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: telegram.ApprovalKeyboard(id),
	})
	if err == nil && sent != nil {
		telegram.MarkAwaitingOwnerInteraction(d.Bot, sent.Chat.ID)
		if a, getErr := d.Queue.Get(ctx, id); getErr == nil {
			d.bindMessage(sent, a.SessionID)
		}
		if rendered.Truncated {
			_, docErr := d.Bot.SendDocument(ctx, &tgbot.SendDocumentParams{
				ChatID:  d.Owner.ID(),
				Caption: "Full approval payload " + id,
				Document: &models.InputFileUpload{
					Filename: "approval-" + id + ".json",
					Data:     bytes.NewReader([]byte(rendered.Full)),
				},
			})
			if docErr != nil {
				d.Log.Warn("send full approval payload", slog.String("id", id), slog.Any("err", docErr))
			}
		}
	}
	return sent, err
}

func (d *Daemon) sendEncryptedApprovalMessage(ctx context.Context, id, tool, inputJSON string, restored bool, renderCtx approvalRenderContext) (*models.Message, error) {
	if strings.TrimSpace(d.EnvelopeSeed) == "" {
		return nil, errors.New("encrypted mode enabled without envelope seed; run `onibi setup --enable-encrypted-mode`")
	}
	rendered := renderApproval(tool, inputJSON, renderCtx)
	body, risk := rendered.Plain, rendered.Risk
	if restored {
		body += "\n\nRe-sent after daemon restart. The original hook may have already proceeded."
	}
	token, err := envelope.Encrypt(d.EnvelopeSeed, envelope.Plain{
		Kind:  "approval",
		ID:    id,
		Title: "Approval request",
		Risk:  risk.Level,
		Body:  body,
	}, renderCtx.ExpiresAt)
	if err != nil {
		return nil, err
	}
	url, err := envelope.BuildMiniAppURL(d.MiniAppURL, token)
	if err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("Encrypted approval request %s.\nOpen the Mini App to decrypt and decide.\n%s", id, formatApprovalExpiry(renderCtx.ExpiresAt))
	sent, err := d.Bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      d.Owner.ID(),
		Text:        msg,
		ReplyMarkup: telegram.EncryptedApprovalKeyboard(url),
	})
	if err == nil && sent != nil {
		telegram.MarkAwaitingOwnerInteraction(d.Bot, sent.Chat.ID)
		if a, getErr := d.Queue.Get(ctx, id); getErr == nil {
			d.bindMessage(sent, a.SessionID)
		}
	}
	return sent, err
}

// respondAndAnnotate edits the Telegram message in place with a decided-state
// label and produces the intake.Response. Decision audit is written by Queue.
func (d *Daemon) respondAndAnnotate(
	ctx context.Context, approvalID string, sent *models.Message,
	dec approval.Decision, ev intake.Event,
) (intake.Response, error) {
	if sent != nil {
		d.editDecisionKeyboard(ctx, sent.Chat.ID, int64(sent.ID), dec)
	}

	resp := intake.Response{
		Decision:  string(dec.Verdict),
		Reason:    dec.Reason,
		DecidedBy: dec.DecidedBy,
	}
	if len(dec.UpdatedInput) > 0 {
		resp.UpdatedInput = string(dec.UpdatedInput)
	}
	return resp, nil
}

// onCallback handles owner-checked [Approve][Deny][Edit] taps. Approve and
// Deny terminate the approval directly. Edit parks the approval awaiting a
// reply-text from the user containing the new JSON.
func (d *Daemon) onCallback(ctx context.Context, api telegram.API, q *models.CallbackQuery, verb, id string) error {
	switch verb {
	case "noop":
		answerCallback(ctx, api, q.ID, "Already decided")
		return nil
	case "menu_status":
		answerCallback(ctx, api, q.ID, "Sending status")
		_, _ = d.sendMaybeEncryptedText(ctx, api, q.From.ID, "status", "Onibi status", d.statusText(ctx, q.From.ID))
		return nil
	case "menu_sessions":
		answerCallback(ctx, api, q.ID, "Sending sessions")
		_, _ = d.sendMaybeEncryptedText(ctx, api, q.From.ID, "sessions", "Active sessions", d.sessionsText(ctx, q.From.ID))
		return nil
	case "menu_queue":
		answerCallback(ctx, api, q.ID, "Sending queue")
		d.handleQueueCommand(ctx, api, q.From.ID, "")
		return nil
	case "menu_secure":
		answerCallback(ctx, api, q.ID, "Opening secure controls")
		d.sendSecureRequired(ctx, api, q.From.ID)
		return nil
	case "menu_projects":
		answerCallback(ctx, api, q.ID, "Sending projects")
		d.handleProjectCommand(ctx, api, q.From.ID, "list")
		return nil
	case "menu_doctor":
		answerCallback(ctx, api, q.ID, "Doctor")
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: "Doctor\nlocal: onibi doctor --explain\nupgrade: onibi doctor --after-upgrade"})
		return nil
	case "menu_hooks":
		answerCallback(ctx, api, q.ID, "Hooks")
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: "Hooks: " + d.hookHealthSummary(ctx) + "\nlocal: onibi hooks show --all\nfix: onibi install-hooks --interactive"})
		return nil
	case "menu_snooze":
		answerCallback(ctx, api, q.ID, "Snoozed")
		d.handleSnoozeCommand(ctx, api, q.From.ID, "")
		return nil
	case "menu_unsnooze":
		answerCallback(ctx, api, q.ID, "Unsnoozed")
		d.handleUnsnoozeCommand(ctx, api, q.From.ID, "")
		return nil
	case "menu_new_headless":
		answerCallback(ctx, api, q.ID, "Headless session")
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: "Start headless:\n/project list\n/new --headless --project <alias> shell\n/new --headless --project <alias> codex"})
		return nil
	case "menu_new_visible":
		answerCallback(ctx, api, q.ID, "Visible session")
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: q.From.ID, Text: "Start visible:\n/project list\n/new --visible --project <alias> shell\n/new --visible --project <alias> codex"})
		return nil
	}
	if verb == "target" {
		return d.handleTargetCallback(ctx, api, q, id)
	}
	if verb == "menu_send" {
		return d.handleMenuSendCallback(ctx, api, q, id)
	}
	switch verb {
	case "prompt_send", "prompt_edit", "prompt_cancel", "prompt_up", "prompt_down":
		return d.handlePromptCallback(ctx, api, q, verb, id)
	case "peek", "text", "render", "show", "hide", "hide_headless", "hide_end", "interrupt", "kill":
		return d.handleSessionActionCallback(ctx, api, q, verb, id)
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		// could be a stale callback (daemon restart, approval already gone)
		answerCallback(ctx, api, q.ID, "Unknown approval")
		return nil
	}
	if a.State != approval.StatePending {
		// already decided — answer with an "already decided" toast
		answerCallback(ctx, api, q.ID, "Already "+a.State)
		return nil
	}
	if !a.ExpiresAt.After(time.Now()) {
		res, err := d.Queue.DecideWithResult(ctx, id, approval.VerdictExpire, "", "approval expired (5 min TTL)", 0)
		if err == nil && !res.Delivered {
			d.editStoredDecision(ctx, a, res.Decision)
		}
		answerCallback(ctx, api, q.ID, "Expired")
		return nil
	}

	switch verb {
	case "approve":
		if isHighRiskApproval(a) {
			if q.Message.Message != nil {
				_, _ = api.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
					ChatID:      q.Message.Message.Chat.ID,
					MessageID:   q.Message.Message.ID,
					ReplyMarkup: telegram.ConfirmApprovalKeyboard(id),
				})
			}
			answerCallback(ctx, api, q.ID, "Confirm high-risk approval")
			return nil
		}
		res, err := d.Queue.DecideWithResult(ctx, id, approval.VerdictApprove, "", "", q.From.ID)
		return d.finishCallbackDecision(ctx, api, q, a, res, err, "Approved")

	case "confirm_approve":
		res, err := d.Queue.DecideWithResult(ctx, id, approval.VerdictApprove, "", "", q.From.ID)
		return d.finishCallbackDecision(ctx, api, q, a, res, err, "Approved")

	case "deny":
		res, err := d.Queue.DecideWithResult(ctx, id, approval.VerdictDeny, "",
			"denied by owner via Telegram", q.From.ID)
		return d.finishCallbackDecision(ctx, api, q, a, res, err, "Denied")

	case "deny_reason":
		if d.encryptedModeEnabled() {
			answerCallback(ctx, api, q.ID, "Use secure controls")
			d.sendSecureRequired(ctx, api, q.From.ID)
			return nil
		}
		d.setPending(ctx, pendingKindDenyReason, q.From.ID, id)
		answerCallback(ctx, api, q.ID, "Send deny reason")
		params := &tgbot.SendMessageParams{
			ChatID: q.From.ID,
			Text:   "Reply with denial reason for approval " + id + ". Reply 'cancel' to abort.",
		}
		if q.Message.Message != nil {
			params.ReplyParameters = &models.ReplyParameters{MessageID: q.Message.Message.ID}
		}
		sendAwaitingMessage(ctx, api, params)
		return nil

	case "edit":
		// park: next text reply from the owner becomes the edited JSON
		d.setPending(ctx, pendingKindApprovalEdit, q.From.ID, id)
		// prompt the user
		answerCallback(ctx, api, q.ID, "Send edited JSON")
		params := &tgbot.SendMessageParams{
			ChatID: q.From.ID,
			Text:   "Reply to this message with edited tool input JSON for approval " + id + ".\nReply 'cancel' to abort edit mode.",
		}
		if q.Message.Message != nil {
			params.ReplyParameters = &models.ReplyParameters{MessageID: q.Message.Message.ID}
		}
		sendAwaitingMessage(ctx, api, params)
		return nil

	default:
		return errors.New("unknown verb: " + verb)
	}
}

// onReply handles a text message that's a reply to one of our approval
// messages. If we have a pending edit for this user, parse the JSON and
// decide the approval as edited.
func (d *Daemon) onReply(ctx context.Context, api telegram.API, m *models.Message) error {
	approvalID, ok := d.takePending(ctx, pendingKindApprovalEdit, m.From.ID)
	if !ok {
		if d.handlePendingPromptEdit(ctx, api, m) {
			return nil
		}
		if d.handlePendingDenyReason(ctx, api, m) {
			return nil
		}
		if d.handlePendingMenuSend(ctx, api, m) {
			return nil
		}
		if d.encryptedModeEnabled() {
			d.sendSecureRequired(ctx, api, m.Chat.ID)
			return nil
		}
		return d.injectTelegramText(ctx, api, m.Chat.ID, d.sessionIDForReply(m), m.Text)
	}
	if d.encryptedModeEnabled() {
		d.sendSecureRequired(ctx, api, m.Chat.ID)
		return nil
	}

	txt := strings.TrimSpace(m.Text)
	if strings.EqualFold(txt, "cancel") {
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Edit cancelled. Tap [Approve] or [Deny] on the original approval.",
		})
		return nil
	}

	editJSON, authErr, authNote := d.prepareApprovalEdit(ctx, m.Chat.ID, txt)
	if authErr != "" {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: authErr})
		d.setPending(ctx, pendingKindApprovalEdit, m.From.ID, approvalID)
		return nil
	}

	// validate JSON
	var anyObj any
	if err := json.Unmarshal([]byte(editJSON), &anyObj); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Invalid JSON: " + err.Error() + "\nReply again with valid JSON, or 'cancel' to abort.",
		})
		// re-park: still awaiting an edit
		d.setPending(ctx, pendingKindApprovalEdit, m.From.ID, approvalID)
		return nil
	}

	a, getErr := d.Queue.Get(ctx, approvalID)
	if getErr != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown approval."})
		return nil
	}
	if err := approval.ValidateEditedInput(a.Tool, a.InputJSON, editJSON); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Invalid edited input: " + err.Error() + "\nReply again with valid JSON, or 'cancel' to abort.",
		})
		d.setPending(ctx, pendingKindApprovalEdit, m.From.ID, approvalID)
		return nil
	}
	res, err := d.Queue.DecideWithResult(ctx, approvalID, approval.VerdictEdit, editJSON, "", m.From.ID)
	if errors.Is(err, approval.ErrExpired) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Approval expired."})
		if !res.Delivered {
			d.editStoredDecision(ctx, a, res.Decision)
		}
		return nil
	}
	if errors.Is(err, approval.ErrAlreadyDecided) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Approval already decided by another path.",
		})
		return nil
	}
	if err != nil {
		return err
	}
	if !res.Delivered {
		d.editStoredDecision(ctx, a, res.Decision)
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{
		ChatID: m.Chat.ID,
		Text:   withTOTPNote("Edited input accepted; tool will run with your version.", authNote),
	})
	return nil
}

func (d *Daemon) handlePendingDenyReason(ctx context.Context, api telegram.API, m *models.Message) bool {
	approvalID, ok := d.takePending(ctx, pendingKindDenyReason, m.From.ID)
	if !ok {
		return false
	}
	if d.encryptedModeEnabled() {
		d.sendSecureRequired(ctx, api, m.Chat.ID)
		return true
	}
	reason := strings.TrimSpace(m.Text)
	if strings.EqualFold(reason, "cancel") {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Deny reason cancelled. Tap Deny for one-tap denial."})
		return true
	}
	if reason == "" {
		d.setPending(ctx, pendingKindDenyReason, m.From.ID, approvalID)
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Deny reason is empty. Reply with a reason, or 'cancel' to abort."})
		return true
	}
	a, err := d.Queue.Get(ctx, approvalID)
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown approval."})
		return true
	}
	res, err := d.Queue.DecideWithResult(ctx, approvalID, approval.VerdictDeny, "", reason, m.From.ID)
	if errors.Is(err, approval.ErrExpired) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Approval expired."})
		if !res.Delivered {
			d.editStoredDecision(ctx, a, res.Decision)
		}
		return true
	}
	if errors.Is(err, approval.ErrAlreadyDecided) {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Approval already decided by another path."})
		return true
	}
	if err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Deny failed: " + err.Error()})
		return true
	}
	if !res.Delivered {
		d.editStoredDecision(ctx, a, res.Decision)
	}
	sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Denied with reason."})
	return true
}

func (d *Daemon) prepareApprovalEdit(ctx context.Context, chatID int64, txt string) (string, string, string) {
	paranoid, err := d.paranoidMode(ctx)
	if err != nil {
		return "", "TOTP unavailable: " + err.Error(), ""
	}
	if !paranoid {
		return txt, "", ""
	}
	if d.withinTOTPGrace(ctx, chatID) {
		return txt, "", "(within TOTP grace)"
	}
	body, code, ok := splitEditTOTP(txt)
	if !ok {
		return "", "Paranoid mode requires edited JSON followed by a 6-digit TOTP code.", ""
	}
	secret, enabled, err := d.totpSecret(ctx)
	if err != nil {
		return "", "TOTP unavailable: " + err.Error(), ""
	}
	if !enabled {
		return "", "TOTP unavailable: paranoid mode is set but TOTP is not configured", ""
	}
	valid, err := auth.Verify(secret, code)
	if err != nil || !valid {
		return "", "Invalid TOTP code.", ""
	}
	d.recordTOTPSuccess(ctx, chatID)
	return body, "", "(60s grace)"
}

func splitEditTOTP(txt string) (string, string, bool) {
	lines := strings.Split(strings.TrimRight(txt, "\r\n\t "), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 {
		return "", "", false
	}
	code := strings.TrimSpace(lines[len(lines)-1])
	if !isTOTPCode(code) {
		return "", "", false
	}
	body := strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n"))
	if body == "" {
		return "", "", false
	}
	return body, code, true
}

func (d *Daemon) onText(ctx context.Context, api telegram.API, m *models.Message) error {
	if m.WebAppData != nil {
		return d.onWebAppData(ctx, api, m)
	}
	if d.handlePendingMenuSend(ctx, api, m) {
		return nil
	}
	text := strings.TrimRight(m.Text, "\r\n")
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "//") {
		if d.encryptedModeEnabled() {
			d.sendSecureRequired(ctx, api, m.Chat.ID)
			return nil
		}
		return d.injectTelegramText(ctx, api, m.Chat.ID, "", strings.TrimPrefix(trimmed, "/"))
	}
	if !strings.HasPrefix(trimmed, "/") && d.handlePendingPromptEdit(ctx, api, m) {
		return nil
	}
	if d.handleTextCommand(ctx, api, m) {
		return nil
	}
	if cmd, _, ok := parseTelegramCommand(m.Text); ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown command: " + cmd})
		return nil
	}
	if d.encryptedModeEnabled() {
		d.sendSecureRequired(ctx, api, m.Chat.ID)
		return nil
	}
	return d.injectTelegramText(ctx, api, m.Chat.ID, "", m.Text)
}

// renderApprovalMessage formats the Telegram message body for an approval
// request. Scrubs secrets from rendered tool inputs.
func renderApprovalMessage(tool, inputJSON, sessLabel string) string {
	return renderApproval(tool, inputJSON, approvalRenderContext{SessionLabel: sessLabel}).HTML
}

func renderApprovalPlainText(tool, inputJSON, sessLabel string) (string, approval.Risk) {
	rendered := renderApproval(tool, inputJSON, approvalRenderContext{SessionLabel: sessLabel})
	return rendered.Plain, rendered.Risk
}

type approvalRenderContext struct {
	Agent        string
	SessionLabel string
	CWD          string
	ToolTarget   string
	Command      string
	FilePath     string
	ExpiresAt    time.Time
}

type approvalRender struct {
	HTML      string
	Plain     string
	Full      string
	Risk      approval.Risk
	Truncated bool
}

func renderApproval(tool, inputJSON string, renderCtx approvalRenderContext) approvalRender {
	pretty, risk := approvalRenderParts(tool, inputJSON)
	renderCtx = completeApprovalRenderContext(tool, inputJSON, renderCtx)
	target := approvalDisplayLine(renderCtx.ToolTarget, 160)
	expiry := formatApprovalExpiry(renderCtx.ExpiresAt)
	htmlPrefix := fmt.Sprintf(
		"Approval request\n"+
			"Agent: %s\n"+
			"Session: %s\n"+
			"Project: %s\n"+
			"Tool: %s\n"+
			"Risk: %s\n"+
			"Target: %s\n"+
			"%s\n",
		telegram.EscapeHTML(renderCtx.Agent),
		telegram.EscapeHTML(renderCtx.SessionLabel),
		telegram.EscapeHTML(renderCtx.CWD),
		telegram.EscapeHTML(tool),
		telegram.EscapeHTML(riskText(risk)),
		telegram.EscapeHTML(target),
		telegram.EscapeHTML(expiry))
	preview, truncated := approvalPreview(pretty, len(htmlPrefix)+len(approvalFullPayloadNotice))
	plain := fmt.Sprintf(
		"Approval request\n"+
			"Agent: %s\n"+
			"Session: %s\n"+
			"Project: %s\n"+
			"Tool: %s\n"+
			"Risk: %s\n"+
			"Target: %s\n"+
			"%s\n\n%s",
		renderCtx.Agent,
		renderCtx.SessionLabel,
		renderCtx.CWD,
		tool,
		riskText(risk),
		target,
		expiry,
		preview)
	if truncated {
		plain += approvalFullPayloadNotice
	}
	html := htmlPrefix + telegram.HTMLPre(preview)
	if truncated {
		html += approvalFullPayloadNotice
	}
	return approvalRender{HTML: html, Plain: plain, Full: pretty, Risk: risk, Truncated: truncated}
}

func completeApprovalRenderContext(tool, inputJSON string, renderCtx approvalRenderContext) approvalRenderContext {
	if renderCtx.Agent == "" {
		renderCtx.Agent = "unknown"
	}
	if renderCtx.SessionLabel == "" {
		renderCtx.SessionLabel = "unknown"
	}
	if renderCtx.CWD == "" {
		renderCtx.CWD = "unknown"
	}
	details := approval.ExtractDetails(tool, inputJSON)
	if renderCtx.Command == "" {
		renderCtx.Command = details.Command
	}
	if renderCtx.FilePath == "" {
		renderCtx.FilePath = details.FilePath
	}
	if renderCtx.ToolTarget == "" {
		renderCtx.ToolTarget = details.Target
	}
	if renderCtx.ToolTarget == "" {
		renderCtx.ToolTarget = "unknown"
	}
	return renderCtx
}

func formatApprovalExpiry(expires time.Time) string {
	if expires.IsZero() {
		return "expires: unknown"
	}
	remain := time.Until(expires).Truncate(time.Second)
	if remain < 0 {
		remain = 0
	}
	return fmt.Sprintf("expires: %s local (%s)", expires.Local().Format("15:04:05"), remain)
}

func approvalRenderParts(tool, inputJSON string) (string, approval.Risk) {
	scrubbed := approval.Scrub(inputJSON)
	risk := approval.ClassifyRisk(tool, inputJSON)
	var anyObj any
	if err := json.Unmarshal([]byte(scrubbed), &anyObj); err == nil {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(anyObj); err == nil {
			return strings.TrimRight(b.String(), "\n"), risk
		}
	}
	return scrubbed, risk
}

const approvalFullPayloadNotice = "\n\nFull payload attached separately."

func approvalPreview(pretty string, htmlOverhead int) (string, bool) {
	budget := telegram.SafeTextLimit - htmlOverhead
	if budget < 0 {
		budget = 0
	}
	if len(telegram.HTMLPre(pretty)) <= budget {
		return pretty, false
	}
	runes := []rune(pretty)
	truncated := false
	for len(runes) > 0 && len(telegram.HTMLPre(string(runes)+"\n... truncated ...")) > budget {
		truncated = true
		next := len(runes) * 3 / 4
		if next == len(runes) {
			next--
		}
		runes = runes[:next]
	}
	return string(runes) + "\n... truncated ...", truncated
}

func approvalDisplayLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if limit <= 0 || len(runes) <= limit {
		return s
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func riskText(r approval.Risk) string {
	if len(r.Reasons) == 0 {
		return r.Level
	}
	return r.Level + " - " + strings.Join(r.Reasons, ", ")
}

func (d *Daemon) finishCallbackDecision(ctx context.Context, api telegram.API, q *models.CallbackQuery, a *approval.Approval, res approval.DecisionResult, err error, okText string) error {
	if errors.Is(err, approval.ErrExpired) {
		answerCallback(ctx, api, q.ID, "Expired")
		if !res.Delivered {
			d.editStoredDecision(ctx, a, res.Decision)
		}
		return nil
	}
	if errors.Is(err, approval.ErrAlreadyDecided) {
		answerCallback(ctx, api, q.ID, "Already decided")
		return nil
	}
	if err != nil {
		return err
	}
	answerCallback(ctx, api, q.ID, okText)
	if !res.Delivered {
		d.editStoredDecision(ctx, a, res.Decision)
	}
	return nil
}

func (d *Daemon) editStoredDecision(ctx context.Context, a *approval.Approval, dec approval.Decision) {
	if a.ChatID == 0 || a.MsgID == 0 {
		return
	}
	d.editDecisionKeyboard(ctx, a.ChatID, a.MsgID, dec)
}

func (d *Daemon) editDecisionKeyboard(ctx context.Context, chatID int64, msgID int64, dec approval.Decision) {
	if d.Bot == nil || chatID == 0 || msgID == 0 {
		return
	}
	_, _ = d.Bot.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
		ChatID:      chatID,
		MessageID:   int(msgID),
		ReplyMarkup: telegram.DecidedKeyboard(decisionLabel(dec)),
	})
}

func decisionLabel(dec approval.Decision) string {
	label := "Decision: " + string(dec.Verdict)
	if dec.Reason != "" {
		label += " - " + dec.Reason
	}
	return label
}

func answerCallback(ctx context.Context, api telegram.API, id, text string) {
	if api == nil || id == "" {
		return
	}
	_, _ = api.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
		CallbackQueryID: id,
		Text:            text,
		ShowAlert:       false,
	})
}

func sendMessage(ctx context.Context, api telegram.API, params *tgbot.SendMessageParams) {
	if api == nil {
		return
	}
	_, _ = api.SendMessage(ctx, params)
}

func sendAwaitingMessage(ctx context.Context, api telegram.API, params *tgbot.SendMessageParams) {
	if api == nil {
		return
	}
	sent, err := api.SendMessage(ctx, params)
	if err == nil && sent != nil {
		telegram.MarkAwaitingOwnerInteraction(api, sent.Chat.ID)
	}
}
