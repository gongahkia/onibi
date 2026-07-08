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
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/chatout"
)

const DefaultBaseURL = "https://discord.com/api/v10"
const MessageChunkLimit = 2000
const ComponentsV2Flag = 1 << 15

const (
	OpDispatch       = 0
	OpHeartbeat      = 1
	OpIdentify       = 2
	OpResume         = 6
	OpReconnect      = 7
	OpInvalidSession = 9
	OpHello          = 10
	OpHeartbeatACK   = 11
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

type Ready struct {
	SessionID        string `json:"session_id"`
	ResumeGatewayURL string `json:"resume_gateway_url"`
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

type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content,omitempty"`
}

type Interaction struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	Type      int    `json:"type"`
	GuildID   string `json:"guild_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	User      struct {
		ID string `json:"id"`
	} `json:"user,omitempty"`
	Member struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member,omitempty"`
	Message struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	} `json:"message,omitempty"`
	Data struct {
		Name          string                 `json:"name"`
		CustomID      string                 `json:"custom_id,omitempty"`
		ComponentType int                    `json:"component_type,omitempty"`
		Options       []InteractionOption    `json:"options,omitempty"`
		Components    []InteractionComponent `json:"components,omitempty"`
	} `json:"data"`
}

type InteractionComponent struct {
	Type       int                    `json:"type"`
	CustomID   string                 `json:"custom_id,omitempty"`
	Value      string                 `json:"value,omitempty"`
	Components []InteractionComponent `json:"components,omitempty"`
}

type InteractionOption struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Value any    `json:"value,omitempty"`
}

type Application struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BotPublic bool   `json:"bot_public"`
	Flags     int    `json:"flags"`
}

type Channel struct {
	ID      string `json:"id"`
	Type    int    `json:"type"`
	GuildID string `json:"guild_id,omitempty"`
	Name    string `json:"name,omitempty"`
}

type ApplicationCommand struct {
	ID          string                     `json:"id,omitempty"`
	Type        int                        `json:"type,omitempty"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Options     []ApplicationCommandOption `json:"options,omitempty"`
}

type ApplicationCommandOption struct {
	Type        int    `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

type APIError struct {
	Path       string
	StatusCode int
	Code       int
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Code != 0 {
		return fmt.Sprintf("discord %s: status %d code %d: %s", e.Path, e.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("discord %s: status %d: %s", e.Path, e.StatusCode, msg)
}

type GatewayState struct {
	mu               sync.Mutex
	Seq              int64
	HasSeq           bool
	SessionID        string
	ResumeGatewayURL string
	AwaitingAck      bool
	LastHeartbeat    time.Time
	LastAck          time.Time
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

func SendHeartbeat(ctx context.Context, c *websocket.Conn, seq *int64) error {
	return writeGateway(ctx, c, GatewayFrame{Op: OpHeartbeat, D: mustJSON(seq)})
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

func ParseHello(frame GatewayFrame) (Hello, bool, error) {
	if frame.Op != OpHello {
		return Hello{}, false, nil
	}
	var hello Hello
	return hello, true, json.Unmarshal(frame.D, &hello)
}

func ParseReady(frame GatewayFrame) (Ready, bool, error) {
	if frame.Op != OpDispatch || frame.T != "READY" {
		return Ready{}, false, nil
	}
	var ready Ready
	return ready, true, json.Unmarshal(frame.D, &ready)
}

func ParseInteraction(frame GatewayFrame) (Interaction, bool, error) {
	if frame.Op != OpDispatch || frame.T != "INTERACTION_CREATE" {
		return Interaction{}, false, nil
	}
	var in Interaction
	return in, true, json.Unmarshal(frame.D, &in)
}

func InteractionText(in Interaction) string {
	for _, opt := range in.Data.Options {
		if opt.Name == "text" {
			if s, ok := opt.Value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func InteractionUserID(in Interaction) string {
	if in.User.ID != "" {
		return in.User.ID
	}
	return in.Member.User.ID
}

func InteractionModalValue(in Interaction, customID string) string {
	for _, component := range in.Data.Components {
		if got := interactionComponentValue(component, customID); got != "" {
			return strings.TrimSpace(got)
		}
	}
	return ""
}

func interactionComponentValue(component InteractionComponent, customID string) string {
	if component.CustomID == customID {
		return component.Value
	}
	for _, child := range component.Components {
		if got := interactionComponentValue(child, customID); got != "" {
			return got
		}
	}
	return ""
}

func (s *GatewayState) Observe(frame GatewayFrame) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if frame.S != nil {
		s.Seq = *frame.S
		s.HasSeq = true
	}
	if frame.Op == OpHeartbeatACK {
		s.AwaitingAck = false
		s.LastAck = time.Now()
		return
	}
	if frame.Op == OpInvalidSession {
		var resumable bool
		if err := json.Unmarshal(frame.D, &resumable); err == nil && !resumable {
			s.SessionID = ""
			s.ResumeGatewayURL = ""
			s.HasSeq = false
			s.Seq = 0
		}
		return
	}
	if frame.Op == OpDispatch && frame.T == "READY" {
		var ready Ready
		if err := json.Unmarshal(frame.D, &ready); err == nil {
			s.SessionID = ready.SessionID
			s.ResumeGatewayURL = ready.ResumeGatewayURL
		}
	}
}

func (s *GatewayState) HeartbeatSeq() *int64 {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.HasSeq {
		return nil
	}
	seq := s.Seq
	return &seq
}

func (s *GatewayState) MarkHeartbeatSent() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.AwaitingAck = true
	s.LastHeartbeat = time.Now()
	s.mu.Unlock()
}

func (s *GatewayState) AckOverdue(timeout time.Duration) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.AwaitingAck && !s.LastHeartbeat.IsZero() && time.Since(s.LastHeartbeat) > timeout
}

func (s *GatewayState) Resume(defaultURL string) (url string, sessionID string, seq int64, ok bool) {
	if s == nil {
		return defaultURL, "", 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SessionID == "" || !s.HasSeq {
		return defaultURL, "", 0, false
	}
	url = strings.TrimSpace(s.ResumeGatewayURL)
	if url == "" {
		url = defaultURL
	}
	return url, s.SessionID, s.Seq, true
}

func (c *Client) CreateMessage(ctx context.Context, channelID, content string) error {
	return c.CreateMessageChunks(ctx, channelID, content, nil)
}

func (c *Client) CreateMessageChunks(ctx context.Context, channelID, content string, after func(int, string)) error {
	for i, chunk := range chatout.Chunks(content, MessageChunkLimit) {
		if i > 0 {
			if err := chatout.Sleep(ctx, 0, c.Sleep); err != nil {
				return err
			}
		}
		if _, err := c.CreateMessagePayload(ctx, channelID, map[string]any{
			"content":          chunk,
			"allowed_mentions": map[string]any{"parse": []string{}},
		}); err != nil {
			return err
		}
		if after != nil {
			after(i, chunk)
		}
	}
	return nil
}

func (c *Client) CreateMessagePayload(ctx context.Context, channelID string, payload map[string]any) (Message, error) {
	if strings.TrimSpace(channelID) == "" {
		return Message{}, errors.New("discord channel id required")
	}
	var out Message
	if err := c.api(ctx, http.MethodPost, "/channels/"+url.PathEscape(channelID)+"/messages", payload, &out); err != nil {
		return Message{}, err
	}
	if out.ID == "" {
		return Message{}, errors.New("discord message id missing")
	}
	return out, nil
}

func (c *Client) CreateComponentsMessage(ctx context.Context, channelID string, components []any) (Message, error) {
	return c.CreateMessagePayload(ctx, channelID, map[string]any{
		"flags":            ComponentsV2Flag,
		"components":       components,
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
}

func (c *Client) StartThreadFromMessage(ctx context.Context, channelID, messageID, name string) (Channel, error) {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(messageID) == "" {
		return Channel{}, errors.New("discord channel/message id required")
	}
	if strings.TrimSpace(name) == "" {
		name = "onibi"
	}
	var out Channel
	p := "/channels/" + url.PathEscape(channelID) + "/messages/" + url.PathEscape(messageID) + "/threads"
	if err := c.api(ctx, http.MethodPost, p, map[string]any{"name": name, "auto_archive_duration": 60}, &out); err != nil {
		return Channel{}, err
	}
	if out.ID == "" {
		return Channel{}, errors.New("discord thread id missing")
	}
	return out, nil
}

func (c *Client) RespondInteraction(ctx context.Context, interactionID, token, content string) error {
	body := map[string]any{"type": 4, "data": map[string]any{"content": content}}
	return c.api(ctx, http.MethodPost, "/interactions/"+url.PathEscape(interactionID)+"/"+url.PathEscape(token)+"/callback", body, nil)
}

func (c *Client) RespondInteractionModal(ctx context.Context, interactionID, token string, modal map[string]any) error {
	body := map[string]any{"type": 9, "data": modal}
	return c.api(ctx, http.MethodPost, "/interactions/"+url.PathEscape(interactionID)+"/"+url.PathEscape(token)+"/callback", body, nil)
}

func (c *Client) CurrentApplication(ctx context.Context) (Application, error) {
	var out Application
	if err := c.api(ctx, http.MethodGet, "/oauth2/applications/@me", nil, &out); err != nil {
		return Application{}, err
	}
	if out.ID == "" {
		return Application{}, errors.New("discord application id missing")
	}
	return out, nil
}

func (c *Client) Channel(ctx context.Context, channelID string) (Channel, error) {
	if strings.TrimSpace(channelID) == "" {
		return Channel{}, errors.New("discord channel id required")
	}
	var out Channel
	if err := c.api(ctx, http.MethodGet, "/channels/"+url.PathEscape(channelID), nil, &out); err != nil {
		return Channel{}, err
	}
	if out.ID == "" {
		return Channel{}, errors.New("discord channel id missing")
	}
	return out, nil
}

func OnibiCommand() ApplicationCommand {
	return ApplicationCommand{
		Type:        1,
		Name:        "onibi",
		Description: "Send terminal input to Onibi",
		Options: []ApplicationCommandOption{{
			Type:        3,
			Name:        "text",
			Description: "Input text",
			Required:    true,
		}},
	}
}

func (c *Client) RegisterOnibiCommand(ctx context.Context, applicationID, guildID string) (ApplicationCommand, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		app, err := c.CurrentApplication(ctx)
		if err != nil {
			return ApplicationCommand{}, err
		}
		applicationID = app.ID
	}
	p := "/applications/" + url.PathEscape(applicationID)
	if strings.TrimSpace(guildID) != "" {
		p += "/guilds/" + url.PathEscape(strings.TrimSpace(guildID))
	}
	p += "/commands"
	var out ApplicationCommand
	if err := c.api(ctx, http.MethodPost, p, OnibiCommand(), &out); err != nil {
		return ApplicationCommand{}, err
	}
	if out.Name == "" {
		return ApplicationCommand{}, errors.New("discord command response missing name")
	}
	return out, nil
}

func (c *Client) ApplicationCommands(ctx context.Context, applicationID, guildID string) ([]ApplicationCommand, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		app, err := c.CurrentApplication(ctx)
		if err != nil {
			return nil, err
		}
		applicationID = app.ID
	}
	p := "/applications/" + url.PathEscape(applicationID)
	if strings.TrimSpace(guildID) != "" {
		p += "/guilds/" + url.PathEscape(strings.TrimSpace(guildID))
	}
	p += "/commands"
	var out []ApplicationCommand
	if err := c.api(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func HasOnibiCommand(commands []ApplicationCommand) bool {
	for _, cmd := range commands {
		if strings.EqualFold(strings.TrimSpace(cmd.Name), "onibi") {
			return true
		}
	}
	return false
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
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		msg := strings.TrimSpace(body.Message)
		if msg == "" {
			msg = resp.Status
		}
		return &APIError{Path: p, StatusCode: resp.StatusCode, Code: body.Code, Message: msg}
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
