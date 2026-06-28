package gotify

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
)

type Client struct {
	BaseURL     string
	AppToken    string
	ClientToken string
	HTTP        *http.Client
}

type Message struct {
	Title    string `json:"title,omitempty"`
	Message  string `json:"message"`
	Priority int    `json:"priority,omitempty"`
}

func New(baseURL, appToken, clientToken string) *Client {
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), AppToken: strings.TrimSpace(appToken), ClientToken: strings.TrimSpace(clientToken), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Send(ctx context.Context, msg Message) error {
	if c == nil {
		return errors.New("gotify client nil")
	}
	if c.BaseURL == "" || c.AppToken == "" {
		return errors.New("gotify url/app token required")
	}
	if strings.TrimSpace(msg.Message) == "" {
		return errors.New("gotify message required")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	reqURL, err := gotifyTokenURL(c.BaseURL+"/message", c.AppToken)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", c.AppToken)
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
		return fmt.Errorf("gotify send status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Validate(ctx context.Context) error {
	if c == nil {
		return errors.New("gotify client nil")
	}
	if c.BaseURL == "" || c.AppToken == "" {
		return errors.New("gotify url/app token required")
	}
	if c.ClientToken == "" {
		return nil
	}
	reqURL, err := gotifyTokenURL(c.BaseURL+"/message?limit=1", c.ClientToken)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Gotify-Key", c.ClientToken)
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
		return fmt.Errorf("gotify validate status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SubscribeWS(ctx context.Context) (*websocket.Conn, error) {
	if c == nil {
		return nil, errors.New("gotify client nil")
	}
	if c.BaseURL == "" || c.ClientToken == "" {
		return nil, errors.New("gotify url/client token required")
	}
	u, err := url.Parse(c.BaseURL + "/stream")
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	q := u.Query()
	q.Set("token", c.ClientToken)
	u.RawQuery = q.Encode()
	conn, _, err := websocket.Dial(ctx, u.String(), nil)
	return conn, err
}

func gotifyTokenURL(raw, token string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
