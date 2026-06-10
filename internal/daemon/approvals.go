package daemon

import (
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
	if ev.Session == "" {
		if s := d.sessionForEvent(ev); s != nil {
			ev.Session = s.ID
			d.appendEventOutput(s, ev)
		}
	}
	// fall back to session label if known (for nicer message header)
	sessLabel := ev.Session
	if s, err := d.Registry.Get(ev.Session); err == nil {
		sessLabel = s.Name + " (" + s.ID + ")"
	}

	approvalID, ch, err := d.Queue.Request(ctx, ev.Session, ev.Agent, ev.Tool, ev.InputJSON)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}

	sent, sendErr := d.sendApprovalMessage(ctx, approvalID, ev.Tool, ev.InputJSON, sessLabel, false)
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
	_ = d.DB.AuditAppend(ctx, "approval.request", ev.Session, ev.InputJSON, 0,
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
		sent, err := d.sendApprovalMessage(ctx, a.ID, a.Tool, a.InputJSON, sessLabel, true)
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

func (d *Daemon) sendApprovalMessage(ctx context.Context, id, tool, inputJSON, sessLabel string, restored bool) (*models.Message, error) {
	if d.Bot == nil {
		return nil, errors.New("telegram bot unavailable")
	}
	msg := renderApprovalMessage(tool, inputJSON, sessLabel)
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
	if verb == "target" {
		return d.handleTargetCallback(ctx, api, q, id)
	}
	switch verb {
	case "prompt_send", "prompt_edit", "prompt_cancel", "prompt_up", "prompt_down":
		return d.handlePromptCallback(ctx, api, q, verb, id)
	case "peek", "interrupt", "kill":
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
		res, err := d.Queue.DecideWithResult(ctx, id, approval.VerdictApprove, "", "", q.From.ID)
		return d.finishCallbackDecision(ctx, api, q, a, res, err, "Approved")

	case "deny":
		res, err := d.Queue.DecideWithResult(ctx, id, approval.VerdictDeny, "",
			"denied by owner via Telegram", q.From.ID)
		return d.finishCallbackDecision(ctx, api, q, a, res, err, "Denied")

	case "edit":
		// park: next text reply from the owner becomes the edited JSON
		d.editMu.Lock()
		d.pendingEdits[q.From.ID] = id
		d.editMu.Unlock()
		// prompt the user
		answerCallback(ctx, api, q.ID, "Send edited JSON")
		params := &tgbot.SendMessageParams{
			ChatID: q.From.ID,
			Text:   "Reply to this message with edited tool input JSON for approval " + id + ".\nReply 'cancel' to abort edit mode.",
		}
		if q.Message.Message != nil {
			params.ReplyParameters = &models.ReplyParameters{MessageID: q.Message.Message.ID}
		}
		sendMessage(ctx, api, params)
		return nil

	default:
		return errors.New("unknown verb: " + verb)
	}
}

// onReply handles a text message that's a reply to one of our approval
// messages. If we have a pending edit for this user, parse the JSON and
// decide the approval as edited.
func (d *Daemon) onReply(ctx context.Context, api telegram.API, m *models.Message) error {
	d.editMu.Lock()
	approvalID, ok := d.pendingEdits[m.From.ID]
	if ok {
		delete(d.pendingEdits, m.From.ID)
	}
	d.editMu.Unlock()
	if !ok {
		if d.handlePendingPromptEdit(ctx, api, m) {
			return nil
		}
		return d.injectTelegramText(ctx, api, m.Chat.ID, d.sessionIDForReply(m), m.Text)
	}

	txt := strings.TrimSpace(m.Text)
	if strings.EqualFold(txt, "cancel") {
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Edit cancelled. Tap [Approve] or [Deny] on the original approval.",
		})
		return nil
	}

	// validate JSON
	var anyObj any
	if err := json.Unmarshal([]byte(txt), &anyObj); err != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Invalid JSON: " + err.Error() + "\nReply again with valid JSON, or 'cancel' to abort.",
		})
		// re-park: still awaiting an edit
		d.editMu.Lock()
		d.pendingEdits[m.From.ID] = approvalID
		d.editMu.Unlock()
		return nil
	}

	a, getErr := d.Queue.Get(ctx, approvalID)
	if getErr != nil {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown approval."})
		return nil
	}
	res, err := d.Queue.DecideWithResult(ctx, approvalID, approval.VerdictEdit, txt, "", m.From.ID)
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
		Text:   "Edited input accepted; tool will run with your version.",
	})
	return nil
}

func (d *Daemon) onText(ctx context.Context, api telegram.API, m *models.Message) error {
	if !strings.HasPrefix(strings.TrimSpace(m.Text), "/") && d.handlePendingPromptEdit(ctx, api, m) {
		return nil
	}
	if d.handleTextCommand(ctx, api, m) {
		return nil
	}
	if cmd, _, ok := parseTelegramCommand(m.Text); ok {
		sendMessage(ctx, api, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: "Unknown command: " + cmd})
		return nil
	}
	return d.injectTelegramText(ctx, api, m.Chat.ID, "", m.Text)
}

// renderApprovalMessage formats the Telegram message body for an approval
// request. Scrubs secrets from rendered tool inputs.
func renderApprovalMessage(tool, inputJSON, sessLabel string) string {
	scrubbed := approval.Scrub(inputJSON)
	// pretty-print the input JSON for readability
	var pretty []byte
	var anyObj any
	if err := json.Unmarshal([]byte(scrubbed), &anyObj); err == nil {
		pretty, _ = json.MarshalIndent(anyObj, "", "  ")
	} else {
		pretty = []byte(scrubbed)
	}
	return fmt.Sprintf(
		"Approval request\n"+
			"Session: %s\n"+
			"Tool: %s\n%s",
		telegram.EscapeHTML(sessLabel), telegram.EscapeHTML(tool), telegram.HTMLPre(string(pretty)))
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
