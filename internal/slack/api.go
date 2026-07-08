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

type AuthTestResponse struct {
	OK     bool   `json:"ok"`
	URL    string `json:"url"`
	Team   string `json:"team"`
	User   string `json:"user"`
	TeamID string `json:"team_id"`
	UserID string `json:"user_id"`
	BotID  string `json:"bot_id"`
	Err    string `json:"error"`
}

type ConversationInfoResponse struct {
	OK      bool   `json:"ok"`
	Err     string `json:"error"`
	Channel struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsChannel bool   `json:"is_channel"`
		IsGroup   bool   `json:"is_group"`
		IsIM      bool   `json:"is_im"`
		IsMember  bool   `json:"is_member"`
	} `json:"channel"`
}

type PostMessageResponse struct {
	OK      bool   `json:"ok"`
	Err     string `json:"error"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

type ViewOpenResponse struct {
	OK   bool   `json:"ok"`
	Err  string `json:"error"`
	View any    `json:"view"`
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
	Type      string `json:"type"`
	TriggerID string `json:"trigger_id"`
	User      struct {
		ID string `json:"id"`
	} `json:"user"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		TS string `json:"ts"`
	} `json:"message"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	View struct {
		ID              string `json:"id"`
		CallbackID      string `json:"callback_id"`
		PrivateMetadata string `json:"private_metadata"`
		State           struct {
			Values map[string]map[string]ViewStateValue `json:"values"`
		} `json:"state"`
	} `json:"view"`
}

type ViewStateValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type DisconnectPayload struct {
	Reason string `json:"reason"`
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
	return c.PostMessageChunks(ctx, channel, text, nil)
}

func (c *Client) PostMessageChunks(ctx context.Context, channel, text string, after func(int, string)) error {
	if strings.TrimSpace(channel) == "" {
		return errors.New("slack channel required")
	}
	pace := c.PostPace
	if pace == 0 {
		pace = DefaultPostPace
	}
	for i, chunk := range chatout.Chunks(text, MessageChunkLimit) {
		if i > 0 {
			if err := chatout.Sleep(ctx, pace, c.Sleep); err != nil {
				return err
			}
		}
		if err := c.api(ctx, "chat.postMessage", c.BotToken, map[string]any{"channel": channel, "text": chunk}, nil); err != nil {
			return err
		}
		if after != nil {
			after(i, chunk)
		}
	}
	return nil
}

func (c *Client) PostMessageBlocks(ctx context.Context, channel, text string, blocks []any) (PostMessageResponse, error) {
	if strings.TrimSpace(channel) == "" {
		return PostMessageResponse{}, errors.New("slack channel required")
	}
	payload := map[string]any{"channel": channel, "text": text}
	if len(blocks) > 0 {
		payload["blocks"] = blocks
	}
	var out PostMessageResponse
	if err := c.api(ctx, "chat.postMessage", c.BotToken, payload, &out); err != nil {
		return PostMessageResponse{}, err
	}
	if !out.OK {
		return PostMessageResponse{}, fmt.Errorf("slack chat.postMessage: %s", out.Err)
	}
	return out, nil
}

func (c *Client) UpdateMessage(ctx context.Context, channel, ts, text string, blocks []any) (PostMessageResponse, error) {
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(ts) == "" {
		return PostMessageResponse{}, errors.New("slack channel/ts required")
	}
	payload := map[string]any{"channel": channel, "ts": ts, "text": text}
	if len(blocks) > 0 {
		payload["blocks"] = blocks
	}
	var out PostMessageResponse
	if err := c.api(ctx, "chat.update", c.BotToken, payload, &out); err != nil {
		return PostMessageResponse{}, err
	}
	if !out.OK {
		return PostMessageResponse{}, fmt.Errorf("slack chat.update: %s", out.Err)
	}
	return out, nil
}

func (c *Client) OpenView(ctx context.Context, triggerID string, view map[string]any) (ViewOpenResponse, error) {
	if strings.TrimSpace(triggerID) == "" {
		return ViewOpenResponse{}, errors.New("slack trigger_id required")
	}
	if len(view) == 0 {
		return ViewOpenResponse{}, errors.New("slack view required")
	}
	var out ViewOpenResponse
	if err := c.api(ctx, "views.open", c.BotToken, map[string]any{"trigger_id": triggerID, "view": view}, &out); err != nil {
		return ViewOpenResponse{}, err
	}
	if !out.OK {
		return ViewOpenResponse{}, fmt.Errorf("slack views.open: %s", out.Err)
	}
	return out, nil
}

func (c *Client) AuthTest(ctx context.Context) (AuthTestResponse, error) {
	var out AuthTestResponse
	if err := c.api(ctx, "auth.test", c.BotToken, nil, &out); err != nil {
		return AuthTestResponse{}, err
	}
	if !out.OK {
		return AuthTestResponse{}, fmt.Errorf("slack auth.test: %s", out.Err)
	}
	return out, nil
}

func (c *Client) ConversationInfo(ctx context.Context, channel string) (ConversationInfoResponse, error) {
	if strings.TrimSpace(channel) == "" {
		return ConversationInfoResponse{}, errors.New("slack channel required")
	}
	var out ConversationInfoResponse
	if err := c.api(ctx, "conversations.info", c.BotToken, map[string]any{"channel": channel}, &out); err != nil {
		return ConversationInfoResponse{}, err
	}
	if !out.OK {
		return ConversationInfoResponse{}, fmt.Errorf("slack conversations.info: %s", out.Err)
	}
	return out, nil
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

func ParseDisconnect(env Envelope) (DisconnectPayload, error) {
	var p DisconnectPayload
	return p, json.Unmarshal(env.Payload, &p)
}

func ShouldReconnect(env Envelope) bool {
	return env.Type == "disconnect"
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
