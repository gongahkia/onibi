package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

const MessageChunkLimit = 3500

type Client struct {
	Homeserver  string
	AccessToken string
	HTTP        *http.Client
	TxnID       func() string
	Sleep       chatout.Sleeper
}

type WhoAmI struct {
	UserID string `json:"user_id"`
}

type JoinedRooms struct {
	JoinedRooms []string `json:"joined_rooms"`
}

type SyncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join map[string]JoinedRoom `json:"join"`
	} `json:"rooms"`
}

type JoinedRoom struct {
	Timeline struct {
		Events []Event `json:"events"`
	} `json:"timeline"`
}

type Event struct {
	Type    string          `json:"type"`
	Sender  string          `json:"sender"`
	Content json.RawMessage `json:"content"`
}

type RoomMessage struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

type PowerLevels struct {
	Users        map[string]int `json:"users"`
	UsersDefault int            `json:"users_default"`
}

func New(homeserver, token string) *Client {
	return &Client{Homeserver: strings.TrimRight(strings.TrimSpace(homeserver), "/"), AccessToken: strings.TrimSpace(token), HTTP: &http.Client{Timeout: 35 * time.Second}}
}

func (c *Client) WhoAmI(ctx context.Context) (WhoAmI, error) {
	var out WhoAmI
	if err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/account/whoami", nil, &out); err != nil {
		return WhoAmI{}, err
	}
	return out, nil
}

func (c *Client) JoinedRooms(ctx context.Context) (JoinedRooms, error) {
	var out JoinedRooms
	if err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/joined_rooms", nil, &out); err != nil {
		return JoinedRooms{}, err
	}
	return out, nil
}

func (c *Client) Sync(ctx context.Context, since string, timeout time.Duration) (SyncResponse, error) {
	q := url.Values{}
	if since != "" {
		q.Set("since", since)
	}
	if timeout > 0 {
		q.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	}
	p := "/_matrix/client/v3/sync"
	if encoded := q.Encode(); encoded != "" {
		p += "?" + encoded
	}
	var out SyncResponse
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return SyncResponse{}, err
	}
	return out, nil
}

func (c *Client) SyncRoom(ctx context.Context, roomID, since string, timeout time.Duration) (SyncResponse, error) {
	if strings.TrimSpace(roomID) == "" {
		return SyncResponse{}, errors.New("matrix room id required")
	}
	filter := map[string]any{
		"room": map[string]any{
			"rooms": []string{roomID},
			"state": map[string]any{"types": []string{"m.room.encryption", "m.room.power_levels"}},
			"timeline": map[string]any{
				"limit": 20,
				"types": []string{"m.room.message", "m.room.encrypted"},
			},
		},
	}
	b, err := json.Marshal(filter)
	if err != nil {
		return SyncResponse{}, err
	}
	q := url.Values{"filter": {string(b)}}
	if since != "" {
		q.Set("since", since)
	}
	if timeout > 0 {
		q.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	}
	var out SyncResponse
	if err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/sync?"+q.Encode(), nil, &out); err != nil {
		return SyncResponse{}, err
	}
	return out, nil
}

func (c *Client) SendText(ctx context.Context, roomID, text string) error {
	return chatout.SendChunks(ctx, text, MessageChunkLimit, 0, c.Sleep, func(ctx context.Context, chunk string) error {
		return c.sendTextChunk(ctx, roomID, chunk)
	})
}

func (c *Client) sendTextChunk(ctx context.Context, roomID, text string) error {
	if strings.TrimSpace(roomID) == "" {
		return errors.New("matrix room id required")
	}
	if strings.TrimSpace(text) == "" {
		text = "(empty)"
	}
	txnID := fmt.Sprintf("%d", time.Now().UnixNano())
	if c.TxnID != nil {
		txnID = c.TxnID()
	}
	p := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/send/m.room.message/" + url.PathEscape(txnID)
	return c.do(ctx, http.MethodPut, p, RoomMessage{MsgType: "m.text", Body: text}, nil)
}

func (c *Client) PowerLevels(ctx context.Context, roomID string) (PowerLevels, error) {
	var out PowerLevels
	p := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/state/m.room.power_levels"
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return PowerLevels{}, err
	}
	if out.Users == nil {
		out.Users = map[string]int{}
	}
	return out, nil
}

func (c *Client) IsEncryptedRoom(ctx context.Context, roomID string) (bool, error) {
	var out map[string]any
	p := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/state/m.room.encryption"
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		if strings.Contains(err.Error(), "M_NOT_FOUND") || strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Client) CheckRoomOwner(ctx context.Context, roomID string, minPower int) (WhoAmI, error) {
	who, err := c.WhoAmI(ctx)
	if err != nil {
		return WhoAmI{}, err
	}
	pl, err := c.PowerLevels(ctx, roomID)
	if err != nil {
		return WhoAmI{}, err
	}
	level, ok := pl.Users[who.UserID]
	if !ok {
		level = pl.UsersDefault
	}
	if level < minPower {
		return who, fmt.Errorf("matrix room power level %d below required %d", level, minPower)
	}
	return who, nil
}

func MessageBody(ev Event) string {
	if ev.Type != "m.room.message" {
		return ""
	}
	var msg RoomMessage
	if err := json.Unmarshal(ev.Content, &msg); err != nil {
		return ""
	}
	if msg.MsgType != "m.text" {
		return ""
	}
	return strings.TrimSpace(msg.Body)
}

func (c *Client) do(ctx context.Context, method, p string, payload any, dst any) error {
	return c.doAttempt(ctx, method, p, payload, dst, 1)
}

func (c *Client) doAttempt(ctx context.Context, method, p string, payload any, dst any, retries int) error {
	if c == nil {
		return errors.New("matrix client nil")
	}
	if c.Homeserver == "" {
		return errors.New("matrix homeserver required")
	}
	if c.AccessToken == "" {
		return errors.New("matrix access token required")
	}
	base, err := url.Parse(c.Homeserver)
	if err != nil {
		return err
	}
	reqURL := *base
	reqURL.Path = path.Join(base.Path, p)
	reqURL.RawQuery = ""
	if i := strings.IndexByte(p, '?'); i >= 0 {
		reqURL.Path = path.Join(base.Path, p[:i])
		reqURL.RawQuery = p[i+1:]
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
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
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
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			ErrCode      string `json:"errcode"`
			Error        string `json:"error"`
			RetryAfterMS int    `json:"retry_after_ms"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode == http.StatusTooManyRequests && retries > 0 {
			delay := time.Duration(e.RetryAfterMS) * time.Millisecond
			if delay <= 0 {
				delay = chatout.RetryAfter(resp.Header, time.Second)
			}
			if err := chatout.Sleep(ctx, delay, c.Sleep); err != nil {
				return err
			}
			return c.doAttempt(ctx, method, p, payload, dst, retries-1)
		}
		msg := strings.TrimSpace(e.Error)
		if msg == "" {
			msg = resp.Status
		}
		if e.ErrCode != "" {
			msg = e.ErrCode + " " + msg
		}
		return fmt.Errorf("matrix %s: %s", p, msg)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
