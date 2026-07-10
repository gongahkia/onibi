package ntfy

import (
	"context"
	"encoding/json"
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
	Title   string
	Body    string
	Tags    string
	Actions []Action
}

type Action struct {
	Type   string
	Label  string
	URL    string
	Method string
	Clear  bool
}

type StreamMessage struct {
	ID      string `json:"id"`
	Time    int64  `json:"time"`
	Event   string `json:"event"`
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type TailOptions struct {
	Since         string
	RetryMin      time.Duration
	RetryMax      time.Duration
	MaxReconnects int
	AfterError    func(error, time.Duration, int)
}

type handlerError struct {
	err error
}

func (e handlerError) Error() string {
	return e.err.Error()
}

func (e handlerError) Unwrap() error {
	return e.err
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
	if weakTopicSecret(topic) {
		return errors.New("ntfy topic must contain enough mixed random characters")
	}
	low := strings.ToLower(topic)
	for _, bad := range []string{"onibi", "approval", "test", "default", "public"} {
		if strings.Contains(low, bad) {
			return fmt.Errorf("ntfy topic contains guessable word %q", bad)
		}
	}
	return nil
}

func weakTopicSecret(topic string) bool {
	classes := map[string]bool{}
	unique := map[rune]bool{}
	for _, r := range topic {
		unique[r] = true
		switch {
		case r >= 'a' && r <= 'z':
			classes["lower"] = true
		case r >= 'A' && r <= 'Z':
			classes["upper"] = true
		case r >= '0' && r <= '9':
			classes["digit"] = true
		default:
			classes["symbol"] = true
		}
	}
	return len(unique) < 8 || len(classes) < 2
}

func (c *Client) Publish(ctx context.Context, msg Message) error {
	_, err := c.PublishMessage(ctx, msg)
	return err
}

func (c *Client) PublishMessage(ctx context.Context, msg Message) (StreamMessage, error) {
	if err := c.validate(); err != nil {
		return StreamMessage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.topicURL(), strings.NewReader(msg.Body))
	if err != nil {
		return StreamMessage{}, err
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
	if len(msg.Actions) > 0 {
		header, err := ActionsHeader(msg.Actions)
		if err != nil {
			return StreamMessage{}, err
		}
		req.Header.Set("X-Actions", header)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return StreamMessage{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return StreamMessage{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StreamMessage{}, fmt.Errorf("ntfy publish status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out StreamMessage
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return StreamMessage{}, err
	}
	return out, nil
}

func ActionsHeader(actions []Action) (string, error) {
	if len(actions) > 3 {
		return "", errors.New("ntfy supports at most 3 actions")
	}
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		part, err := actionHeader(action)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; "), nil
}

func actionHeader(action Action) (string, error) {
	typ := strings.ToLower(strings.TrimSpace(action.Type))
	label := strings.TrimSpace(action.Label)
	target := strings.TrimSpace(action.URL)
	if label == "" {
		return "", errors.New("ntfy action label required")
	}
	switch typ {
	case "view":
		if target == "" {
			return "", errors.New("ntfy view action url required")
		}
		out := "view, " + quoteActionValue(label) + ", " + quoteActionValue(target)
		if action.Clear {
			out += ", clear=true"
		}
		return out, nil
	case "http":
		if target == "" {
			return "", errors.New("ntfy http action url required")
		}
		method := strings.ToUpper(strings.TrimSpace(action.Method))
		if method == "" {
			method = http.MethodPost
		}
		out := "http, " + quoteActionValue(label) + ", " + quoteActionValue(target) + ", method=" + method
		if action.Clear {
			out += ", clear=true"
		}
		return out, nil
	default:
		return "", fmt.Errorf("unsupported ntfy action type %q", action.Type)
	}
}

func quoteActionValue(value string) string {
	if !strings.ContainsAny(value, `,;"`) {
		return value
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func (c *Client) StreamJSON(ctx context.Context, since string, handle func(StreamMessage) error) error {
	_, err := c.streamJSON(ctx, since, handle)
	return err
}

func (c *Client) TailJSON(ctx context.Context, opts TailOptions, handle func(StreamMessage) error) error {
	minBackoff := opts.RetryMin
	if minBackoff <= 0 {
		minBackoff = 250 * time.Millisecond
	}
	maxBackoff := opts.RetryMax
	if maxBackoff <= 0 || maxBackoff < minBackoff {
		maxBackoff = 2 * time.Second
	}
	backoff := minBackoff
	since := strings.TrimSpace(opts.Since)
	reconnects := 0
	for {
		lastID, err := c.streamJSON(ctx, since, handle)
		var handled handlerError
		if errors.As(err, &handled) {
			return handled.err
		}
		if lastID != "" {
			since = lastID
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !errors.Is(err, io.EOF) {
			if opts.MaxReconnects > 0 && reconnects >= opts.MaxReconnects {
				return err
			}
		} else if opts.MaxReconnects > 0 && reconnects >= opts.MaxReconnects {
			return nil
		}
		reconnects++
		if opts.AfterError != nil {
			opts.AfterError(err, backoff, reconnects)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (c *Client) streamJSON(ctx context.Context, since string, handle func(StreamMessage) error) (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	if handle == nil {
		return "", errors.New("ntfy stream handler required")
	}
	u, err := url.Parse(c.topicURL() + "/json")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(since) != "" {
		q := u.Query()
		q.Set("since", strings.TrimSpace(since))
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ntfy subscribe status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	dec := json.NewDecoder(resp.Body)
	lastID := ""
	for {
		var msg StreamMessage
		if err := dec.Decode(&msg); err != nil {
			return lastID, err
		}
		if msg.ID != "" {
			lastID = msg.ID
		}
		if err := handle(msg); err != nil {
			return lastID, handlerError{err: err}
		}
	}
}

func (c *Client) SubscribeWS(ctx context.Context) (*websocket.Conn, error) {
	return c.SubscribeWSSince(ctx, "")
}

func (c *Client) SubscribeWSSince(ctx context.Context, since string) (*websocket.Conn, error) {
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
	if strings.TrimSpace(since) != "" {
		q := u.Query()
		q.Set("since", strings.TrimSpace(since))
		u.RawQuery = q.Encode()
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
