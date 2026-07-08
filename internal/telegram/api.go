package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultBaseURL = "https://api.telegram.org"
const GlobalSendInterval = time.Second / 30
const ChatSendInterval = time.Second

var botTokenRE = regexp.MustCompile(`^[0-9]{5,12}:[A-Za-z0-9_-]{30,}$`)

type Client struct {
	Token      string
	BaseURL    string
	HTTP       *http.Client
	RetrySleep func(context.Context, time.Duration) error
	MaxRetries int

	mu         sync.Mutex
	nextGlobal time.Time
	nextChat   map[int64]time.Time
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	From      *User  `json:"from"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

func NewClient(token string) *Client {
	return &Client{Token: strings.TrimSpace(token), BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 35 * time.Second}}
}

func ValidBotToken(token string) bool {
	return botTokenRE.MatchString(strings.TrimSpace(token))
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	var out User
	if err := c.callJSON(ctx, "getMe", nil, &out); err != nil {
		return User{}, err
	}
	return out, nil
}

func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.callJSON(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	if timeout <= 0 {
		timeout = 25
	}
	req := map[string]any{
		"timeout": timeout,
		"allowed_updates": []string{
			"message",
			"callback_query",
		},
	}
	if offset > 0 {
		req["offset"] = offset
	}
	var out []Update
	if err := c.callJSON(ctx, "getUpdates", req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, markup *InlineKeyboardMarkup) (Message, error) {
	if strings.TrimSpace(text) == "" {
		text = "(empty)"
	}
	if err := c.waitSendLimit(ctx, chatID); err != nil {
		return Message{}, err
	}
	req := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	var out Message
	if err := c.callJSON(ctx, "sendMessage", req, &out); err != nil {
		return Message{}, err
	}
	return out, nil
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, png []byte, caption string) error {
	if len(png) == 0 {
		return errors.New("photo bytes required")
	}
	if err := c.waitSendLimit(ctx, chatID); err != nil {
		return err
	}
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if caption != "" {
		_ = w.WriteField("caption", caption)
	}
	part, err := w.CreateFormFile("photo", "onibi.png")
	if err != nil {
		return err
	}
	if _, err := part.Write(png); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.callMultipart(ctx, "sendPhoto", w.FormDataContentType(), body, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string) error {
	req := map[string]any{"callback_query_id": id}
	if text != "" {
		req["text"] = text
	}
	return c.callJSON(ctx, "answerCallbackQuery", req, nil)
}

func (c *Client) callJSON(ctx context.Context, method string, payload any, dst any) error {
	var body io.Reader
	if payload == nil {
		body = strings.NewReader("{}")
	} else {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	return c.do(ctx, method, "application/json", body, dst)
}

func (c *Client) callMultipart(ctx context.Context, method, contentType string, body io.Reader, dst any) error {
	return c.do(ctx, method, contentType, body, dst)
}

func (c *Client) do(ctx context.Context, method, contentType string, body io.Reader, dst any) error {
	if c == nil {
		return errors.New("telegram client nil")
	}
	if !ValidBotToken(c.Token) {
		return errors.New("invalid Telegram bot token")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.JoinPath(base, "bot"+c.Token, method)
	if err != nil {
		return err
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	maxRetries := c.MaxRetries
	if maxRetries == 0 {
		maxRetries = 1
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", contentType)
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		env, err := decodeResponseEnvelope(resp)
		if err != nil {
			return err
		}
		if (!env.OK || resp.StatusCode >= 400) && resp.StatusCode == http.StatusTooManyRequests && env.Parameters != nil && env.Parameters.RetryAfter > 0 && attempt < maxRetries {
			if err := c.sleepRetry(ctx, time.Duration(env.Parameters.RetryAfter)*time.Second); err != nil {
				return err
			}
			continue
		}
		if !env.OK || resp.StatusCode >= 400 {
			msg := strings.TrimSpace(env.Description)
			if msg == "" {
				msg = resp.Status
			}
			return fmt.Errorf("telegram %s: %s", method, msg)
		}
		if dst == nil {
			return nil
		}
		if len(env.Result) == 0 {
			return nil
		}
		return json.Unmarshal(env.Result, dst)
	}
}

type responseParameters struct {
	RetryAfter int `json:"retry_after"`
}

type responseEnvelope struct {
	OK          bool                `json:"ok"`
	Result      json.RawMessage     `json:"result"`
	Description string              `json:"description"`
	ErrorCode   int                 `json:"error_code"`
	Parameters  *responseParameters `json:"parameters"`
}

func decodeResponseEnvelope(resp *http.Response) (responseEnvelope, error) {
	defer resp.Body.Close()
	var env responseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return env, err
	}
	return env, nil
}

func (c *Client) sleepRetry(ctx context.Context, d time.Duration) error {
	if c.RetrySleep != nil {
		return c.RetrySleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) waitSendLimit(ctx context.Context, chatID int64) error {
	if c == nil {
		return errors.New("telegram client nil")
	}
	now := time.Now()
	c.mu.Lock()
	if c.nextChat == nil {
		c.nextChat = map[int64]time.Time{}
	}
	waitUntil := c.nextGlobal
	if next := c.nextChat[chatID]; next.After(waitUntil) {
		waitUntil = next
	}
	delay := time.Duration(0)
	slot := now
	if waitUntil.After(now) {
		delay = waitUntil.Sub(now)
		slot = waitUntil
	}
	c.nextGlobal = slot.Add(GlobalSendInterval)
	c.nextChat[chatID] = slot.Add(ChatSendInterval)
	c.mu.Unlock()
	return c.sleepRetry(ctx, delay)
}
