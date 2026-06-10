package telegram

import (
	"context"
	"log/slog"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/auth"
)

// CallbackHandler is invoked for every owner-authorized callback query.
// verb + id are parsed from CallbackData via ParseCallback.
type CallbackHandler func(ctx context.Context, api API, q *models.CallbackQuery, verb, id string) error

// MessageHandler is invoked for every owner-authorized text message.
type MessageHandler func(ctx context.Context, api API, m *models.Message) error

// Router wires owner-checked handlers onto the bot. It is the single
// chokepoint per §11 rule 13: every inbound update is filtered through
// MustBeOwner before reaching any handler.
type Router struct {
	Owner   *auth.Owner
	Log     *slog.Logger
	OnCB    CallbackHandler
	OnText  MessageHandler
	OnReply MessageHandler // when message.reply_to_message is set
	dropped uint64         // counter of non-owner drops (informational)
}

// Dispatch is the single entry point invoked by the telegram client's
// DefaultHandler. It does owner-check + routing.
func (r *Router) Dispatch(ctx context.Context, api API, update *models.Update) {
	if r == nil || r.Owner == nil {
		return
	}
	if r.Log == nil {
		r.Log = slog.Default()
	}

	// callback query path
	if update.CallbackQuery != nil {
		q := update.CallbackQuery
		if q.From.ID == 0 || !r.Owner.MustBeOwner(q.From.ID) {
			r.dropped++
			r.ackOnly(ctx, api, q.ID)
			r.Log.Warn("dropped non-owner callback", slog.Int64("from", q.From.ID))
			return
		}
		verb, id := ParseCallback(q.Data)
		if verb == "" {
			r.ackOnly(ctx, api, q.ID)
			r.Log.Debug("unknown callback data", slog.String("data", q.Data))
			return
		}
		if r.OnCB == nil {
			r.ackOnly(ctx, api, q.ID)
			return
		}
		if err := r.OnCB(ctx, api, q, verb, id); err != nil {
			r.ackOnly(ctx, api, q.ID)
			r.Log.Warn("callback handler", slog.String("verb", verb), slog.Any("err", err))
		}
		return
	}

	// text message path
	if update.Message != nil {
		m := update.Message
		if m.From == nil || !r.Owner.MustBeOwner(m.From.ID) {
			r.dropped++
			r.Log.Warn("dropped non-owner message", slog.Int64("from", m.From.ID))
			return
		}
		if m.ReplyToMessage != nil && r.OnReply != nil {
			if err := r.OnReply(ctx, api, m); err != nil {
				r.Log.Warn("reply handler", slog.Any("err", err))
			}
			return
		}
		if r.OnText != nil {
			if err := r.OnText(ctx, api, m); err != nil {
				r.Log.Warn("text handler", slog.Any("err", err))
			}
		}
	}
}

// Dropped returns the running count of non-owner updates rejected at the
// chokepoint. Useful for `onibi doctor` to surface "someone is messaging
// your bot but it isn't you" signals.
func (r *Router) Dropped() uint64 { return r.dropped }

// ackOnly answers a callback query with a generic spinner-clear, no alert.
// Used when we drop the update so the requester doesn't see a stuck spinner.
// (Telegram won't deliver the AnswerCallbackQuery if from != owner anyway,
// but this is harmless and keeps the bot responsive if the attacker uses
// the same bot somehow.)
func (r *Router) ackOnly(ctx context.Context, api API, id string) {
	if api == nil {
		return
	}
	_, _ = api.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: id})
}
