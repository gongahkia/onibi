package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

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
	// fall back to session label if known (for nicer message header)
	sessLabel := ev.Session
	if s, err := d.Registry.Get(ev.Session); err == nil {
		sessLabel = s.Name + " (" + s.ID + ")"
	}

	approvalID, ch, err := d.Queue.Request(ctx, ev.Session, "", ev.Tool, ev.InputJSON)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}

	// render message
	msg := renderApprovalMessage(ev.Tool, ev.InputJSON, sessLabel)
	sent, sendErr := d.Bot.Bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      d.Owner.ID(),
		Text:        msg,
		ParseMode:   models.ParseModeMarkdown,
		ReplyMarkup: telegram.ApprovalKeyboard(approvalID),
	})
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

// respondAndAnnotate edits the Telegram message in place with a decided-
// state label, writes an audit row, and produces the intake.Response.
func (d *Daemon) respondAndAnnotate(
	ctx context.Context, approvalID string, sent *models.Message,
	dec approval.Decision, ev intake.Event,
) (intake.Response, error) {
	label := "Decision: " + string(dec.Verdict)
	if dec.Reason != "" {
		label += " — " + dec.Reason
	}

	// best-effort message edit so user sees the outcome inline
	if sent != nil && d.Bot != nil && d.Bot.Bot != nil {
		_, _ = d.Bot.Bot.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{
			ChatID:      sent.Chat.ID,
			MessageID:   sent.ID,
			ReplyMarkup: telegram.DecidedKeyboard(label),
		})
	}

	// audit
	_ = d.DB.AuditAppend(ctx, "approval.decided", ev.Session, string(dec.UpdatedInput), dec.DecidedBy,
		fmt.Sprintf("id=%s verdict=%s", approvalID, dec.Verdict))

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
func (d *Daemon) onCallback(ctx context.Context, b *tgbot.Bot, q *models.CallbackQuery, verb, id string) error {
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		// could be a stale callback (daemon restart, approval already gone)
		return nil
	}
	if a.State != approval.StatePending {
		// already decided — answer with an "already decided" toast
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: q.ID,
			Text:            "Already " + a.State,
			ShowAlert:       false,
		})
		return nil
	}

	switch verb {
	case "approve":
		return d.Queue.Decide(ctx, id, approval.VerdictApprove, "", "", q.From.ID)

	case "deny":
		return d.Queue.Decide(ctx, id, approval.VerdictDeny, "",
			"denied by owner via Telegram", q.From.ID)

	case "edit":
		// park: next text reply from the owner becomes the edited JSON
		d.editMu.Lock()
		d.pendingEdits[q.From.ID] = id
		d.editMu.Unlock()
		// prompt the user
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: q.From.ID,
			Text:   "Reply to this message with the edited tool input JSON for approval `" + id + "`.\nReply 'cancel' to abort the edit and re-decide.",
			ReplyParameters: &models.ReplyParameters{
				MessageID: q.Message.Message.ID,
			},
		})
		return nil

	default:
		return errors.New("unknown verb: " + verb)
	}
}

// onReply handles a text message that's a reply to one of our approval
// messages. If we have a pending edit for this user, parse the JSON and
// decide the approval as edited.
func (d *Daemon) onReply(ctx context.Context, b *tgbot.Bot, m *models.Message) error {
	d.editMu.Lock()
	approvalID, ok := d.pendingEdits[m.From.ID]
	if ok {
		delete(d.pendingEdits, m.From.ID)
	}
	d.editMu.Unlock()
	if !ok {
		// not awaiting an edit — Phase 6 will route reply-to-message
		// into a session inject
		return nil
	}

	txt := m.Text
	if txt == "cancel" {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Edit cancelled. Tap [Approve] or [Deny] on the original approval.",
		})
		return nil
	}

	// validate JSON
	var anyObj any
	if err := json.Unmarshal([]byte(txt), &anyObj); err != nil {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Invalid JSON: " + err.Error() + "\nReply again with valid JSON, or 'cancel' to abort.",
		})
		// re-park: still awaiting an edit
		d.editMu.Lock()
		d.pendingEdits[m.From.ID] = approvalID
		d.editMu.Unlock()
		return nil
	}

	err := d.Queue.Decide(ctx, approvalID, approval.VerdictEdit, txt, "", m.From.ID)
	if errors.Is(err, approval.ErrAlreadyDecided) {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: m.Chat.ID,
			Text:   "Approval already decided by another path.",
		})
		return nil
	}
	if err != nil {
		return err
	}
	_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: m.Chat.ID,
		Text:   "Edited input accepted; tool will run with your version.",
	})
	return nil
}

// onText is the catch-all for non-reply, non-callback owner messages.
// Phase 6 will use this for /sessions, /target, free-text injection, etc.
// Phase 3 just logs them.
func (d *Daemon) onText(_ context.Context, _ *tgbot.Bot, m *models.Message) error {
	d.Log.Debug("text from owner (no handler wired yet)", slog.String("text", m.Text))
	return nil
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
		"*Approval request*\n"+
			"Session: `%s`\n"+
			"Tool: `%s`\n"+
			"```json\n%s\n```",
		sessLabel, tool, string(pretty))
}
