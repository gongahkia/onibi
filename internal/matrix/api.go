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
)

type Client struct {
	Homeserver  string
	AccessToken string
	HTTP        *http.Client
	TxnID       func() string
}

type WhoAmI struct {
	UserID string `json:"user_id"`
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

func (c *Client) SendText(ctx context.Context, roomID, text string) error {
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
			ErrCode string `json:"errcode"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		msg := strings.TrimSpace(e.Error)
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("matrix %s: %s", p, msg)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
