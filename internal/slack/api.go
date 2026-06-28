package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/chatout"
)

const DefaultBaseURL = "https://slack.com/api"

const (
	MessageChunkLimit = 3000
	DefaultPostPace   = time.Second
)

type Client struct {
	AppToken string
	BotToken string
	BaseURL  string
	HTTP     *http.Client
	Sleep    chatout.Sleeper
	PostPace time.Duration
}

type SocketOpenResponse struct {
	OK  bool   `json:"ok"`
	URL string `json:"url"`
	Err string `json:"error"`
}

type Envelope struct {
	EnvelopeID   string          `json:"envelope_id"`
	Type         string          `json:"type"`
	Accepts      bool            `json:"accepts_response_payload"`
	Payload      json.RawMessage `json:"payload"`
	RetryAttempt int             `json:"retry_attempt"`
	RetryReason  string          `json:"retry_reason"`
}

type EventPayload struct {
	Event struct {
		Type        string `json:"type"`
		Channel     string `json:"channel"`
		User        string `json:"user"`
		Text        string `json:"text"`
		ChannelType string `json:"channel_type"`
	} `json:"event"`
	Authorizations []struct {
		UserID string `json:"user_id"`
	} `json:"authorizations"`
}

type InteractionPayload struct {
	Type string `json:"type"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

type Allowlist struct {
	Channels map[string]bool
	DMUsers  map[string]bool
}

func New(appToken, botToken string) *Client {
	return &Client{AppToken: strings.TrimSpace(appToken), BotToken: strings.TrimSpace(botToken), BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) OpenSocket(ctx context.Context) (string, error) {
	var out SocketOpenResponse
	if err := c.api(ctx, "apps.connections.open", c.AppToken, nil, &out); err != nil {
		return "", err
	}
	if !out.OK {
		return "", fmt.Errorf("slack apps.connections.open: %s", out.Err)
	}
	if out.URL == "" {
		return "", errors.New("slack socket url missing")
	}
	return out.URL, nil
}

func (c *Client) PostMessage(ctx context.Context, channel, text string) error {
	if strings.TrimSpace(channel) == "" {
		return errors.New("slack channel required")
	}
	pace := c.PostPace
	if pace == 0 {
		pace = DefaultPostPace
	}
	return chatout.SendChunks(ctx, text, MessageChunkLimit, pace, c.Sleep, func(ctx context.Context, chunk string) error {
		return c.api(ctx, "chat.postMessage", c.BotToken, map[string]any{"channel": channel, "text": chunk}, nil)
	})
}

func Dial(ctx context.Context, socketURL string) (*websocket.Conn, error) {
	c, _, err := websocket.Dial(ctx, socketURL, nil)
	return c, err
}

func ReadEnvelope(ctx context.Context, c *websocket.Conn) (Envelope, error) {
	var env Envelope
	_, p, err := c.Read(ctx)
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(p, &env); err != nil {
		return env, err
	}
	return env, nil
}

func Ack(ctx context.Context, c *websocket.Conn, envelopeID string, payload any) error {
	body := map[string]any{"envelope_id": envelopeID}
	if payload != nil {
		body["payload"] = payload
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func ParseEvent(env Envelope) (EventPayload, error) {
	var p EventPayload
	return p, json.Unmarshal(env.Payload, &p)
}

func ParseInteraction(env Envelope) (InteractionPayload, error) {
	var p InteractionPayload
	return p, json.Unmarshal(env.Payload, &p)
}

func (a Allowlist) Allows(channelID, userID, channelType string) bool {
	if len(a.Channels) == 0 && len(a.DMUsers) == 0 {
		return true
	}
	if a.Channels[channelID] {
		return true
	}
	if channelType == "im" && a.DMUsers[userID] {
		return true
	}
	return false
}

func (c *Client) api(ctx context.Context, method, token string, payload any, dst any) error {
	return c.apiAttempt(ctx, method, token, payload, dst, 1)
}

func (c *Client) apiAttempt(ctx context.Context, method, token string, payload any, dst any, retries int) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("slack token required")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.JoinPath(base, method)
	if err != nil {
		return err
	}
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader([]byte("{}"))
	} else {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		if retries <= 0 {
			return fmt.Errorf("slack %s rate limited", method)
		}
		if err := chatout.Sleep(ctx, chatout.RetryAfter(resp.Header, time.Second), c.Sleep); err != nil {
			return err
		}
		return c.apiAttempt(ctx, method, token, payload, dst, retries-1)
	}
	var raw struct {
		OK  bool            `json:"ok"`
		Err string          `json:"error"`
		URL string          `json:"url"`
		Raw json.RawMessage `json:"-"`
	}
	if dst == nil {
		dst = &raw
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return err
	}
	if checker, ok := dst.(*SocketOpenResponse); ok && !checker.OK {
		return fmt.Errorf("slack %s: %s", method, checker.Err)
	}
	if checker, ok := dst.(*struct {
		OK  bool            `json:"ok"`
		Err string          `json:"error"`
		URL string          `json:"url"`
		Raw json.RawMessage `json:"-"`
	}); ok && !checker.OK {
		return fmt.Errorf("slack %s: %s", method, checker.Err)
	}
	return nil
}
