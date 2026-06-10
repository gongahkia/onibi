package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// API is the daemon-facing Telegram surface. Keep this narrow so tests can
// use Mock without depending on the real Telegram network client.
type API interface {
	Start(context.Context)
	Self() *models.User
	SendMessage(context.Context, *tgbot.SendMessageParams) (*models.Message, error)
	SendPhoto(context.Context, *tgbot.SendPhotoParams) (*models.Message, error)
	EditMessageReplyMarkup(context.Context, *tgbot.EditMessageReplyMarkupParams) (*models.Message, error)
	AnswerCallbackQuery(context.Context, *tgbot.AnswerCallbackQueryParams) (bool, error)
	SetMyCommands(context.Context, *tgbot.SetMyCommandsParams) (bool, error)
}

// HandlerFunc is the API-aware update handler used by Router.
type HandlerFunc func(context.Context, API, *models.Update)
