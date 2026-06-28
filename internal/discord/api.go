package discord

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

const DefaultBaseURL = "https://discord.com/api/v10"
const MessageChunkLimit = 2000

const (
	OpDispatch       = 0
	OpHeartbeat      = 1
	OpIdentify       = 2
	OpResume         = 6
	OpReconnect      = 7
	OpInvalidSession = 9
	OpHello          = 10
)

type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
	Sleep   chatout.Sleeper
}

type GatewayFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

type Hello struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type Identify struct {
	Token      string            `json:"token"`
	Intents    int               `json:"intents"`
	Properties map[string]string `json:"properties"`
}

type Resume struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

type MessageCreate struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id,omitempty"`
	Author    struct {
		ID string `json:"id"`
	} `json:"author"`
	Content string `json:"content"`
}

type Interaction struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Type    int    `json:"type"`
	GuildID string `json:"guild_id,omitempty"`
	Data    struct {
		Name string `json:"name"`
	} `json:"data"`
}

func New(token string) *Client {
	return &Client{Token: strings.TrimSpace(token), BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func DialGateway(ctx context.Context, gatewayURL string) (*websocket.Conn, error) {
	c, _, err := websocket.Dial(ctx, gatewayURL, nil)
	return c, err
}

func ReadFrame(ctx context.Context, c *websocket.Conn) (GatewayFrame, error) {
	var frame GatewayFrame
	_, p, err := c.Read(ctx)
	if err != nil {
		return frame, err
	}
	return frame, json.Unmarshal(p, &frame)
}

func SendIdentify(ctx context.Context, c *websocket.Conn, token string, intents int) error {
	return writeGateway(ctx, c, GatewayFrame{Op: OpIdentify, D: mustJSON(Identify{
		Token:      token,
		Intents:    intents,
		Properties: map[string]string{"os": "onibi", "browser": "onibi", "device": "onibi"},
	})})
}

func SendResume(ctx context.Context, c *websocket.Conn, token, sessionID string, seq int64) error {
	return writeGateway(ctx, c, GatewayFrame{Op: OpResume, D: mustJSON(Resume{Token: token, SessionID: sessionID, Seq: seq})})
}

func HandleReconnect(frame GatewayFrame) bool {
	return frame.Op == OpReconnect || frame.Op == OpInvalidSession
}

func MissingMessageContent(m MessageCreate) bool {
	return strings.TrimSpace(m.Content) == ""
}

func IsDM(m MessageCreate) bool {
	return m.GuildID == ""
}

func ParseMessage(frame GatewayFrame) (MessageCreate, bool, error) {
	if frame.Op != OpDispatch || frame.T != "MESSAGE_CREATE" {
		return MessageCreate{}, false, nil
	}
	var msg MessageCreate
	return msg, true, json.Unmarshal(frame.D, &msg)
}

func ParseInteraction(frame GatewayFrame) (Interaction, bool, error) {
	if frame.Op != OpDispatch || frame.T != "INTERACTION_CREATE" {
		return Interaction{}, false, nil
	}
	var in Interaction
	return in, true, json.Unmarshal(frame.D, &in)
}

func (c *Client) CreateMessage(ctx context.Context, channelID, content string) error {
	return chatout.SendChunks(ctx, content, MessageChunkLimit, 0, c.Sleep, func(ctx context.Context, chunk string) error {
		return c.api(ctx, http.MethodPost, "/channels/"+url.PathEscape(channelID)+"/messages", map[string]any{
			"content":          chunk,
			"allowed_mentions": map[string]any{"parse": []string{}},
		}, nil)
	})
}

func (c *Client) RespondInteraction(ctx context.Context, interactionID, token, content string) error {
	body := map[string]any{"type": 4, "data": map[string]any{"content": content}}
	return c.api(ctx, http.MethodPost, "/interactions/"+url.PathEscape(interactionID)+"/"+url.PathEscape(token)+"/callback", body, nil)
}

func writeGateway(ctx context.Context, c *websocket.Conn, frame GatewayFrame) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (c *Client) api(ctx context.Context, method, p string, payload any, dst any) error {
	return c.apiAttempt(ctx, method, p, payload, dst, 1)
}

func (c *Client) apiAttempt(ctx context.Context, method, p string, payload any, dst any, retries int) error {
	if c == nil {
		return errors.New("discord client nil")
	}
	if c.Token == "" {
		return errors.New("discord token required")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.JoinPath(base, p)
	if err != nil {
		return err
	}
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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
			return fmt.Errorf("discord %s rate limited", p)
		}
		var body struct {
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		delay := time.Duration(body.RetryAfter * float64(time.Second))
		if delay <= 0 {
			delay = chatout.RetryAfter(resp.Header, time.Second)
		}
		if err := chatout.Sleep(ctx, delay, c.Sleep); err != nil {
			return err
		}
		return c.apiAttempt(ctx, method, p, payload, dst, retries-1)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		msg := strings.TrimSpace(body.Message)
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("discord %s: %s", p, msg)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
