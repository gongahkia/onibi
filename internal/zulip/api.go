package zulip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Email   string
	APIKey  string
	HTTP    *http.Client
}

type StreamMessage struct {
	Stream  string
	Topic   string
	Content string
}

type MessageResponse struct {
	ID int64 `json:"id"`
}

type QueueOptions struct {
	EventTypes []string
	Narrow     [][]string
}

type Queue struct {
	QueueID         string `json:"queue_id"`
	LastEventID     int64  `json:"last_event_id"`
	LongPollTimeout int    `json:"event_queue_longpoll_timeout_seconds"`
	IdleTimeout     int    `json:"idle_queue_timeout_secs"`
}

type EventsResponse struct {
	Events []Event `json:"events"`
}

type Event struct {
	ID       int64     `json:"id"`
	Type     string    `json:"type"`
	Message  *Message  `json:"message,omitempty"`
	Reaction *Reaction `json:"reaction,omitempty"`
}

type Message struct {
	ID               int64  `json:"id"`
	Type             string `json:"type"`
	SenderEmail      string `json:"sender_email"`
	SenderFullName   string `json:"sender_full_name"`
	StreamID         int64  `json:"stream_id"`
	DisplayRecipient any    `json:"display_recipient"`
	Subject          string `json:"subject"`
	TopicName        string `json:"topic"`
	Content          string `json:"content"`
}

func (m Message) Topic() string {
	if strings.TrimSpace(m.TopicName) != "" {
		return strings.TrimSpace(m.TopicName)
	}
	return strings.TrimSpace(m.Subject)
}

type Reaction struct {
	MessageID int64  `json:"message_id"`
	UserID    int64  `json:"user_id"`
	EmojiName string `json:"emoji_name"`
}

type APIError struct {
	Status int
	Code   string
	Msg    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("zulip api status=%d code=%s msg=%s", e.Status, e.Code, e.Msg)
	}
	return fmt.Sprintf("zulip api status=%d msg=%s", e.Status, e.Msg)
}

type TailOptions struct {
	QueueOptions
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

func New(baseURL, email, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Email:   strings.TrimSpace(email),
		APIKey:  strings.TrimSpace(apiKey),
		HTTP:    &http.Client{Timeout: 35 * time.Second},
	}
}

func (c *Client) SendStreamMessage(ctx context.Context, msg StreamMessage) (MessageResponse, error) {
	msg.Stream = strings.TrimSpace(msg.Stream)
	msg.Topic = strings.TrimSpace(msg.Topic)
	msg.Content = strings.TrimSpace(msg.Content)
	if msg.Stream == "" || msg.Topic == "" || msg.Content == "" {
		return MessageResponse{}, errors.New("zulip stream, topic, and content required")
	}
	values := url.Values{
		"type":    {"stream"},
		"to":      {msg.Stream},
		"topic":   {msg.Topic},
		"content": {msg.Content},
	}
	var out MessageResponse
	if err := c.doForm(ctx, http.MethodPost, "/api/v1/messages", values, &out); err != nil {
		return MessageResponse{}, err
	}
	return out, nil
}

func (c *Client) AddReaction(ctx context.Context, messageID int64, emojiName string) error {
	emojiName = strings.TrimSpace(emojiName)
	if messageID <= 0 || emojiName == "" {
		return errors.New("zulip message id and emoji required")
	}
	return c.doForm(ctx, http.MethodPost, "/api/v1/messages/"+strconv.FormatInt(messageID, 10)+"/reactions", url.Values{"emoji_name": {emojiName}}, nil)
}

func (c *Client) RegisterQueue(ctx context.Context, opts QueueOptions) (Queue, error) {
	values := url.Values{"apply_markdown": {"false"}}
	if len(opts.EventTypes) > 0 {
		b, err := json.Marshal(opts.EventTypes)
		if err != nil {
			return Queue{}, err
		}
		values.Set("event_types", string(b))
	}
	if len(opts.Narrow) > 0 {
		b, err := json.Marshal(opts.Narrow)
		if err != nil {
			return Queue{}, err
		}
		values.Set("narrow", string(b))
	}
	var out Queue
	if err := c.doForm(ctx, http.MethodPost, "/api/v1/register", values, &out); err != nil {
		return Queue{}, err
	}
	if strings.TrimSpace(out.QueueID) == "" {
		return Queue{}, errors.New("zulip queue id missing")
	}
	return out, nil
}

func (c *Client) GetEvents(ctx context.Context, queueID string, lastEventID int64) (EventsResponse, error) {
	queueID = strings.TrimSpace(queueID)
	if queueID == "" {
		return EventsResponse{}, errors.New("zulip queue id required")
	}
	values := url.Values{
		"queue_id":      {queueID},
		"last_event_id": {strconv.FormatInt(lastEventID, 10)},
	}
	var out EventsResponse
	if err := c.doForm(ctx, http.MethodGet, "/api/v1/events", values, &out); err != nil {
		return EventsResponse{}, err
	}
	return out, nil
}

func (c *Client) DeleteQueue(ctx context.Context, queueID string) error {
	queueID = strings.TrimSpace(queueID)
	if queueID == "" {
		return nil
	}
	return c.doForm(ctx, http.MethodDelete, "/api/v1/events", url.Values{"queue_id": {queueID}}, nil)
}

func (c *Client) TailEvents(ctx context.Context, opts TailOptions, handle func(Event) error) error {
	if handle == nil {
		return errors.New("zulip event handler required")
	}
	minBackoff := opts.RetryMin
	if minBackoff <= 0 {
		minBackoff = 500 * time.Millisecond
	}
	maxBackoff := opts.RetryMax
	if maxBackoff <= 0 || maxBackoff < minBackoff {
		maxBackoff = 5 * time.Second
	}
	backoff := minBackoff
	reconnects := 0
	for {
		err := c.consumeQueue(ctx, opts.QueueOptions, handle)
		var handled handlerError
		if errors.As(err, &handled) {
			return handled.err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if opts.MaxReconnects > 0 && reconnects >= opts.MaxReconnects {
			return err
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

func (c *Client) consumeQueue(ctx context.Context, opts QueueOptions, handle func(Event) error) error {
	queue, err := c.RegisterQueue(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = c.DeleteQueue(context.Background(), queue.QueueID) }()
	last := queue.LastEventID
	for {
		resp, err := c.GetEvents(ctx, queue.QueueID, last)
		if err != nil {
			return err
		}
		for _, ev := range resp.Events {
			if ev.ID > last {
				last = ev.ID
			}
			if ev.Type == "heartbeat" {
				continue
			}
			if err := handle(ev); err != nil {
				return handlerError{err: err}
			}
		}
	}
}

func (c *Client) doForm(ctx context.Context, method, path string, values url.Values, result any) error {
	if err := c.validate(); err != nil {
		return err
	}
	var body io.Reader
	reqURL := c.BaseURL + path
	if method == http.MethodGet {
		u, err := url.Parse(reqURL)
		if err != nil {
			return err
		}
		u.RawQuery = values.Encode()
		reqURL = u.String()
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Email, c.APIKey)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Result string `json:"result"`
		Msg    string `json:"msg"`
		Code   string `json:"code"`
	}
	if len(bytes.TrimSpace(b)) > 0 {
		_ = json.Unmarshal(b, &envelope)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || envelope.Result == "error" {
		if envelope.Msg == "" {
			envelope.Msg = strings.TrimSpace(string(b))
		}
		return &APIError{Status: resp.StatusCode, Code: envelope.Code, Msg: envelope.Msg}
	}
	if result == nil {
		return nil
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	return json.Unmarshal(b, result)
}

func (c *Client) validate() error {
	if c == nil {
		return errors.New("zulip client nil")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("zulip base URL required")
	}
	if c.Email == "" || c.APIKey == "" {
		return errors.New("zulip email and api key required")
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
