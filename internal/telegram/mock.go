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
	SentPhotos   []tgbot.SendPhotoParams
	SentDocs     []tgbot.SendDocumentParams
	Edits        []tgbot.EditMessageReplyMarkupParams
	Answers      []tgbot.AnswerCallbackQueryParams
	Commands     []tgbot.SetMyCommandsParams
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

// SendPhoto records params and returns a synthetic Telegram message.
func (m *Mock) SendPhoto(_ context.Context, params *tgbot.SendPhotoParams) (*models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *params
	m.SentPhotos = append(m.SentPhotos, cp)
	id := m.nextID
	m.nextID++
	chatID, _ := params.ChatID.(int64)
	return &models.Message{
		ID:      id,
		Date:    int(time.Now().Unix()),
		Chat:    models.Chat{ID: chatID, Type: "private"},
		Caption: params.Caption,
	}, nil
}

// SendDocument records params and returns a synthetic Telegram message.
func (m *Mock) SendDocument(_ context.Context, params *tgbot.SendDocumentParams) (*models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *params
	m.SentDocs = append(m.SentDocs, cp)
	id := m.nextID
	m.nextID++
	chatID, _ := params.ChatID.(int64)
	return &models.Message{
		ID:      id,
		Date:    int(time.Now().Unix()),
		Chat:    models.Chat{ID: chatID, Type: "private"},
		Caption: params.Caption,
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

// SetMyCommands records command registration.
func (m *Mock) SetMyCommands(_ context.Context, params *tgbot.SetMyCommandsParams) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *params
	m.Commands = append(m.Commands, cp)
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

// Photos returns a snapshot of sent photos.
func (m *Mock) Photos() []tgbot.SendPhotoParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tgbot.SendPhotoParams, len(m.SentPhotos))
	copy(out, m.SentPhotos)
	return out
}

// Documents returns a snapshot of sent documents.
func (m *Mock) Documents() []tgbot.SendDocumentParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tgbot.SendDocumentParams, len(m.SentDocs))
	copy(out, m.SentDocs)
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

// RegisteredCommands returns a snapshot of set-my-commands calls.
func (m *Mock) RegisteredCommands() []tgbot.SetMyCommandsParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tgbot.SetMyCommandsParams, len(m.Commands))
	copy(out, m.Commands)
	return out
}
