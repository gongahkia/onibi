package ntfy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const DefaultBaseURL = "https://ntfy.sh"

var topicSecretRE = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

type Client struct {
	BaseURL string
	Topic   string
	Token   string
	HTTP    *http.Client
}

type Message struct {
	Title string
	Body  string
	Tags  string
}

func New(baseURL, topic, token string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Topic: strings.TrimSpace(topic), Token: strings.TrimSpace(token), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func ValidateTopicSecret(topic string) error {
	topic = strings.TrimSpace(topic)
	if !topicSecretRE.MatchString(topic) {
		return errors.New("ntfy topic must be a 20+ char random secret using letters, numbers, _ or -")
	}
	low := strings.ToLower(topic)
	for _, bad := range []string{"onibi", "approval", "test", "default", "public"} {
		if strings.Contains(low, bad) {
			return fmt.Errorf("ntfy topic contains guessable word %q", bad)
		}
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, msg Message) error {
	if err := c.validate(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.topicURL(), strings.NewReader(msg.Body))
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if msg.Title != "" {
		req.Header.Set("Title", msg.Title)
	}
	if msg.Tags != "" {
		req.Header.Set("Tags", msg.Tags)
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ntfy publish status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *Client) SubscribeWS(ctx context.Context) (*websocket.Conn, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	u, err := url.Parse(c.topicURL() + "/ws")
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	opts := &websocket.DialOptions{}
	if c.Token != "" {
		opts.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + c.Token}}
	}
	conn, _, err := websocket.Dial(ctx, u.String(), opts)
	return conn, err
}

func (c *Client) validate() error {
	if c == nil {
		return errors.New("ntfy client nil")
	}
	if err := ValidateTopicSecret(c.Topic); err != nil {
		return err
	}
	return nil
}

func (c *Client) topicURL() string {
	return strings.TrimRight(c.BaseURL, "/") + "/" + url.PathEscape(c.Topic)
}
