package telegram

import (
	"context"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Mock is an in-memory Telegram API for daemon/router tests.
type Mock struct {
	mu      sync.Mutex
	self    *models.User
	handler HandlerFunc
	nextID  int

	SentMessages []tgbot.SendMessageParams
	Edits        []tgbot.EditMessageReplyMarkupParams
	Answers      []tgbot.AnswerCallbackQueryParams
}

// NewMock returns a Mock with a stable bot identity.
func NewMock(self *models.User) *Mock {
	if self == nil {
		self = &models.User{ID: 1, Username: "onibi_mock_bot", IsBot: true}
	}
	return &Mock{self: self, nextID: 1}
}

// SetHandler wires an API-aware update handler, usually Router.Dispatch.
func (m *Mock) SetHandler(h HandlerFunc) { m.handler = h }

// Start blocks until ctx is cancelled.
func (m *Mock) Start(ctx context.Context) { <-ctx.Done() }

// Self returns the mock bot identity.
func (m *Mock) Self() *models.User { return m.self }

// SendMessage records params and returns a synthetic Telegram message.
func (m *Mock) SendMessage(_ context.Context, params *tgbot.SendMessageParams) (*models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *params
	m.SentMessages = append(m.SentMessages, cp)
	id := m.nextID
	m.nextID++
	chatID, _ := params.ChatID.(int64)
	return &models.Message{
		ID:   id,
		Date: int(time.Now().Unix()),
		Chat: models.Chat{ID: chatID, Type: "private"},
		Text: params.Text,
	}, nil
}

// EditMessageReplyMarkup records params and returns a synthetic message.
func (m *Mock) EditMessageReplyMarkup(_ context.Context, params *tgbot.EditMessageReplyMarkupParams) (*models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *params
	m.Edits = append(m.Edits, cp)
	chatID, _ := params.ChatID.(int64)
	return &models.Message{
		ID:   params.MessageID,
		Date: int(time.Now().Unix()),
		Chat: models.Chat{ID: chatID, Type: "private"},
	}, nil
}

// AnswerCallbackQuery records params.
func (m *Mock) AnswerCallbackQuery(_ context.Context, params *tgbot.AnswerCallbackQueryParams) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *params
	m.Answers = append(m.Answers, cp)
	return true, nil
}

// Dispatch injects an update into the mock handler.
func (m *Mock) Dispatch(ctx context.Context, update *models.Update) {
	if m.handler != nil {
		m.handler(ctx, m, update)
	}
}

// Sent returns a snapshot of sent messages.
func (m *Mock) Sent() []tgbot.SendMessageParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tgbot.SendMessageParams, len(m.SentMessages))
	copy(out, m.SentMessages)
	return out
}

// Edited returns a snapshot of edit-message-reply-markup calls.
func (m *Mock) Edited() []tgbot.EditMessageReplyMarkupParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tgbot.EditMessageReplyMarkupParams, len(m.Edits))
	copy(out, m.Edits)
	return out
}

// Answered returns a snapshot of callback answers.
func (m *Mock) Answered() []tgbot.AnswerCallbackQueryParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tgbot.AnswerCallbackQueryParams, len(m.Answers))
	copy(out, m.Answers)
	return out
}
