package telegram

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// HTTPTimeout covers the 30s long-poll + slack.
const HTTPTimeout = 35 * time.Second

const (
	longPollTimeout             = 30 * time.Second
	maxPollBackoff              = 5 * time.Second
	ownerRaceEmptyPollThreshold = 10
	ownerInteractionWindow      = 5 * time.Minute
	ownerRaceWarningText        = "Possible token race: another poller may be consuming Telegram updates. Run onibi doctor; if it reports another poller, run onibi rotate-token."
)

// AllowedUpdateTypes restricts the parsing surface to the update kinds we
// actually handle. Matches tgterm's allowed_updates.
var AllowedUpdateTypes = []string{"message", "callback_query"}

// Client wraps github.com/go-telegram/bot with our enforcement layer:
// dedicated http.Client with TLS 1.2 floor, no env proxy honored for
// api.telegram.org, defensive deleteWebhook on Init, getMe identity check.
type Client struct {
	Bot               *tgbot.Bot
	self              *models.User
	token             string
	hc                *http.Client
	allowed           []string
	limiter           *RateLimiter
	clearedWebhookURL string
	poll              getUpdatesFunc
	sleep             sleepFunc
	warningSender     func(context.Context, int64, string) error
	await             ownerInteractionTracker
}

type getUpdatesFunc func(context.Context, int64, time.Duration, []string) ([]*models.Update, error)

type sleepFunc func(context.Context, time.Duration) bool

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

	c = &Client{Bot: b, token: opts.Token, hc: hc, allowed: AllowedUpdateTypes, limiter: DefaultRateLimiter()}

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

// Start enters the owned long-polling loop until ctx is cancelled.
func (c *Client) Start(ctx context.Context) { c.pollLoop(ctx) }

// Self returns the getMe identity cached during New.
func (c *Client) Self() *models.User { return c.self }

func (c *Client) ClearedWebhookURL() string { return c.clearedWebhookURL }

// SendMessage delegates to the real bot.
func (c *Client) SendMessage(ctx context.Context, params *tgbot.SendMessageParams) (*models.Message, error) {
	params = NormalizeSendMessageParams(params)
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatIDKey(params.ChatID)); err != nil {
			return nil, err
		}
	}
	return c.Bot.SendMessage(ctx, params)
}

// SendPhoto delegates to the real bot after rate limiting.
func (c *Client) SendPhoto(ctx context.Context, params *tgbot.SendPhotoParams) (*models.Message, error) {
	params = NormalizeSendPhotoParams(params)
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatIDKey(params.ChatID)); err != nil {
			return nil, err
		}
	}
	return c.Bot.SendPhoto(ctx, params)
}

// SendDocument delegates to the real bot after rate limiting.
func (c *Client) SendDocument(ctx context.Context, params *tgbot.SendDocumentParams) (*models.Message, error) {
	params = NormalizeSendDocumentParams(params)
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, chatIDKey(params.ChatID)); err != nil {
			return nil, err
		}
	}
	return c.Bot.SendDocument(ctx, params)
}

// EditMessageText delegates to the real bot.
func (c *Client) EditMessageText(ctx context.Context, params *tgbot.EditMessageTextParams) (*models.Message, error) {
	params = NormalizeEditMessageTextParams(params)
	return c.Bot.EditMessageText(ctx, params)
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

func (c *Client) pollLoop(ctx context.Context) {
	if c == nil || c.Bot == nil {
		return
	}
	poll := c.poll
	if poll == nil {
		poll = c.fetchUpdates
	}
	var offset int64
	var backoff time.Duration
	for {
		if ctx.Err() != nil {
			return
		}
		if backoff > 0 && !c.sleepContext(ctx, backoff) {
			return
		}
		updates, err := poll(ctx, offset, longPollTimeout, c.allowed)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			backoff = nextPollBackoff(backoff)
			continue
		}
		backoff = 0
		if len(updates) == 0 {
			c.noteEmptyPoll(ctx)
			continue
		}
		c.await.NoteInbound()
		maxID := offset - 1
		for _, update := range updates {
			if update == nil {
				continue
			}
			if update.ID > maxID {
				maxID = update.ID
			}
			c.Bot.ProcessUpdate(ctx, update)
		}
		if maxID >= offset {
			offset = maxID + 1
		}
	}
}

func (c *Client) fetchUpdates(ctx context.Context, offset int64, timeout time.Duration, allowed []string) ([]*models.Update, error) {
	var updates []*models.Update
	err := rawBotCall(ctx, c.hc, c.token, "getUpdates", map[string]any{
		"offset":          offset,
		"limit":           100,
		"timeout":         int(timeout.Seconds()),
		"allowed_updates": allowed,
	}, &updates)
	return updates, err
}

func nextPollBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return 100 * time.Millisecond
	}
	d *= 2
	if d > maxPollBackoff {
		return maxPollBackoff
	}
	return d
}

func (c *Client) sleepContext(ctx context.Context, d time.Duration) bool {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (c *Client) noteEmptyPoll(ctx context.Context) {
	chatID, warn := c.await.NoteEmptyPoll()
	if !warn {
		return
	}
	_ = c.sendRaceWarning(ctx, chatID)
}

func (c *Client) sendRaceWarning(ctx context.Context, chatID int64) error {
	if c.warningSender != nil {
		return c.warningSender(ctx, chatID, ownerRaceWarningText)
	}
	_, err := c.Send(ctx, chatID, ownerRaceWarningText)
	return err
}

// AwaitOwnerInteraction arms the empty-poll race warning after an outbound
// message that expects an owner reply or callback.
func (c *Client) AwaitOwnerInteraction(chatID int64, window time.Duration) {
	c.await.Mark(chatID, window)
}

type ownerInteractionTracker struct {
	mu         sync.Mutex
	chatID     int64
	expires    time.Time
	emptyPolls int
	warned     bool
	now        func() time.Time
}

func (t *ownerInteractionTracker) Mark(chatID int64, window time.Duration) {
	if chatID == 0 {
		return
	}
	if window <= 0 {
		window = ownerInteractionWindow
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chatID = chatID
	t.expires = t.clock().Add(window)
	t.emptyPolls = 0
	t.warned = false
}

func (t *ownerInteractionTracker) NoteInbound() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chatID = 0
	t.expires = time.Time{}
	t.emptyPolls = 0
	t.warned = false
}

func (t *ownerInteractionTracker) NoteEmptyPoll() (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.chatID == 0 {
		return 0, false
	}
	if !t.expires.IsZero() && !t.clock().Before(t.expires) {
		t.chatID = 0
		t.expires = time.Time{}
		t.emptyPolls = 0
		t.warned = false
		return 0, false
	}
	t.emptyPolls++
	if t.emptyPolls >= ownerRaceEmptyPollThreshold && !t.warned {
		t.warned = true
		return t.chatID, true
	}
	return 0, false
}

func (t *ownerInteractionTracker) Awaiting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chatID != 0 && (t.expires.IsZero() || t.clock().Before(t.expires))
}

func (t *ownerInteractionTracker) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

type ownerInteractionAPI interface {
	AwaitOwnerInteraction(int64, time.Duration)
}

// MarkAwaitingOwnerInteraction arms race detection for APIs that support it.
func MarkAwaitingOwnerInteraction(api API, chatID int64) {
	if watcher, ok := api.(ownerInteractionAPI); ok {
		watcher.AwaitOwnerInteraction(chatID, ownerInteractionWindow)
	}
}
