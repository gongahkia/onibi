package telegram

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// HTTPTimeout covers the 30s long-poll + slack. See TODO §7.3.
const HTTPTimeout = 35 * time.Second

// AllowedUpdateTypes restricts the parsing surface to the update kinds we
// actually handle. Matches tgterm's allowed_updates.
var AllowedUpdateTypes = []string{"message", "callback_query"}

// Client wraps github.com/go-telegram/bot with our enforcement layer:
// dedicated http.Client with TLS 1.2 floor, no env proxy honored for
// api.telegram.org, defensive deleteWebhook on Init, getMe identity check.
type Client struct {
	Bot               *tgbot.Bot
	self              *models.User
	allowed           []string
	limiter           *RateLimiter
	clearedWebhookURL string
}

// Options configures Client construction.
type Options struct {
	// Token is the BotFather token. Required.
	Token string
	// AllowEnvProxy honors HTTP_PROXY for api.telegram.org. Off by default
	// to keep the trust boundary tight.
	AllowEnvProxy bool
	// DefaultHandler runs for any update that no registered handler
	// matches. Optional; useful for the pair-wizard wait loop.
	DefaultHandler tgbot.HandlerFunc
	// APIHandler is the daemon/test handler path. If set, it takes
	// precedence over DefaultHandler.
	APIHandler HandlerFunc
}

// New constructs a Client. Calls getMe to populate Self, and proactively
// deletes any pre-existing webhook (threat T1 — attacker token might have
// flipped the bot to webhook mode).
func New(ctx context.Context, opts Options) (*Client, error) {
	if opts.Token == "" {
		return nil, errors.New("telegram: empty token")
	}
	hc := &http.Client{
		Timeout: HTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			Proxy:                 noProxy(opts.AllowEnvProxy),
			ResponseHeaderTimeout: HTTPTimeout,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	var c *Client
	botOpts := []tgbot.Option{
		tgbot.WithHTTPClient(HTTPTimeout, hc),
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates(AllowedUpdateTypes)),
	}
	if opts.APIHandler != nil {
		botOpts = append(botOpts, tgbot.WithDefaultHandler(func(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
			opts.APIHandler(ctx, c, update)
		}))
	} else if opts.DefaultHandler != nil {
		botOpts = append(botOpts, tgbot.WithDefaultHandler(opts.DefaultHandler))
	}
	b, err := tgbot.New(opts.Token, botOpts...)
	if err != nil {
		return nil, fmt.Errorf("telegram new: %w", err)
	}

	c = &Client{Bot: b, allowed: AllowedUpdateTypes, limiter: DefaultRateLimiter()}

	// getMe — populates Self and validates the token in one call
	me, err := b.GetMe(ctx)
	if err != nil {
		return nil, fmt.Errorf("telegram getMe: %w", err)
	}
	if me == nil {
		return nil, errors.New("telegram getMe returned nil")
	}
	c.self = me

	info, err := b.GetWebhookInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("telegram getWebhookInfo: %w", err)
	}
	if info != nil {
		c.clearedWebhookURL = info.URL
	}

	// defensive deleteWebhook so an attacker who flipped to webhook mode
	// loses the side channel. Drop pending updates too — they may be
	// poisoned by attacker traffic.
	if _, err := b.DeleteWebhook(ctx, &tgbot.DeleteWebhookParams{DropPendingUpdates: true}); err != nil {
		return nil, fmt.Errorf("telegram deleteWebhook: %w", err)
	}

	return c, nil
}

// noProxy returns a http.Transport.Proxy function that refuses to use any
// env-supplied proxy for api.telegram.org unless allow is true.
func noProxy(allow bool) func(*http.Request) (*url.URL, error) {
	envProxy := http.ProxyFromEnvironment
	return func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == "api.telegram.org" && !allow {
			return nil, nil
		}
		return envProxy(req)
	}
}

// Send is a thin shortcut for sendMessage in plain text. Callers using
// keyboards or formatting should call c.Bot directly.
func (c *Client) Send(ctx context.Context, chatID int64, text string) (*models.Message, error) {
	return c.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
}

// Start enters the long-polling loop until ctx is cancelled.
func (c *Client) Start(ctx context.Context) { c.Bot.Start(ctx) }

// Self returns the getMe identity cached during New.
func (c *Client) Self() *models.User { return c.self }

func (c *Client) ClearedWebhookURL() string { return c.clearedWebhookURL }

// SendMessage delegates to the real bot.
func (c *Client) SendMessage(ctx context.Context, params *tgbot.SendMessageParams) (*models.Message, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatIDKey(params.ChatID)); err != nil {
			return nil, err
		}
	}
	return c.Bot.SendMessage(ctx, params)
}

// SendPhoto delegates to the real bot after rate limiting.
func (c *Client) SendPhoto(ctx context.Context, params *tgbot.SendPhotoParams) (*models.Message, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatIDKey(params.ChatID)); err != nil {
			return nil, err
		}
	}
	return c.Bot.SendPhoto(ctx, params)
}

// SendDocument delegates to the real bot after rate limiting.
func (c *Client) SendDocument(ctx context.Context, params *tgbot.SendDocumentParams) (*models.Message, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatIDKey(params.ChatID)); err != nil {
			return nil, err
		}
	}
	return c.Bot.SendDocument(ctx, params)
}

// EditMessageReplyMarkup delegates to the real bot.
func (c *Client) EditMessageReplyMarkup(ctx context.Context, params *tgbot.EditMessageReplyMarkupParams) (*models.Message, error) {
	return c.Bot.EditMessageReplyMarkup(ctx, params)
}

// AnswerCallbackQuery delegates to the real bot.
func (c *Client) AnswerCallbackQuery(ctx context.Context, params *tgbot.AnswerCallbackQueryParams) (bool, error) {
	return c.Bot.AnswerCallbackQuery(ctx, params)
}

// SetMyCommands delegates to the real bot.
func (c *Client) SetMyCommands(ctx context.Context, params *tgbot.SetMyCommandsParams) (bool, error) {
	return c.Bot.SetMyCommands(ctx, params)
}
