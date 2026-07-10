package signal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type Client struct {
	BaseURL string
	Account string
	HTTP    *http.Client
	nextID  atomic.Uint64
}

type SendRequest struct {
	Recipients []string
	GroupID    string
	Message    string
}

type SendResult struct {
	Timestamp int64 `json:"timestamp"`
}

type ReactionRequest struct {
	Recipients      []string
	GroupID         string
	Emoji           string
	TargetAuthor    string
	TargetTimestamp int64
	Remove          bool
}

type Envelope struct {
	Source       string       `json:"source"`
	SourceNumber string       `json:"sourceNumber"`
	SourceUUID   string       `json:"sourceUuid"`
	SourceName   string       `json:"sourceName"`
	Timestamp    int64        `json:"timestamp"`
	DataMessage  *DataMessage `json:"dataMessage,omitempty"`
	SyncMessage  any          `json:"syncMessage,omitempty"`
}

type DataMessage struct {
	Timestamp int64           `json:"timestamp"`
	Message   string          `json:"message"`
	Reaction  json.RawMessage `json:"reaction,omitempty"`
}

type Event struct {
	Method       string
	Account      string
	Subscription *int
	Envelope     Envelope
	Raw          json.RawMessage
}

type TailOptions struct {
	RetryMin      time.Duration
	RetryMax      time.Duration
	MaxReconnects int
	AfterError    func(error, time.Duration, int)
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("signal rpc %d: %s", e.Code, e.Message)
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

func New(baseURL, account string) *Client {
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Account: strings.TrimSpace(account), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Check(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/check", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("signal check status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	if strings.TrimSpace(req.Message) == "" {
		return SendResult{}, errors.New("signal message required")
	}
	if !hasTarget(req.Recipients, req.GroupID) {
		return SendResult{}, errors.New("signal recipient or group required")
	}
	params := c.targetParams(req.Recipients, req.GroupID)
	params["message"] = req.Message
	var out SendResult
	if err := c.Call(ctx, "send", params, &out); err != nil {
		return SendResult{}, err
	}
	return out, nil
}

func (c *Client) SendReaction(ctx context.Context, req ReactionRequest) (SendResult, error) {
	if strings.TrimSpace(req.Emoji) == "" && !req.Remove {
		return SendResult{}, errors.New("signal reaction emoji required")
	}
	if strings.TrimSpace(req.TargetAuthor) == "" || req.TargetTimestamp == 0 {
		return SendResult{}, errors.New("signal reaction target required")
	}
	if !hasTarget(req.Recipients, req.GroupID) {
		return SendResult{}, errors.New("signal recipient or group required")
	}
	params := c.targetParams(req.Recipients, req.GroupID)
	params["targetAuthor"] = req.TargetAuthor
	params["targetTimestamp"] = req.TargetTimestamp
	if req.Remove {
		params["remove"] = true
	} else {
		params["emoji"] = req.Emoji
	}
	var out SendResult
	if err := c.Call(ctx, "sendReaction", params, &out); err != nil {
		return SendResult{}, err
	}
	return out, nil
}

func (c *Client) SubscribeReceive(ctx context.Context) (int, error) {
	var id int
	if err := c.Call(ctx, "subscribeReceive", map[string]any{}, &id); err != nil {
		return 0, err
	}
	return id, nil
}

func (c *Client) Call(ctx context.Context, method string, params map[string]any, result any) error {
	if err := c.validate(); err != nil {
		return err
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return errors.New("signal rpc method required")
	}
	rpcParams := make(map[string]any, len(params)+1)
	for k, v := range params {
		rpcParams[k] = v
	}
	if c.Account != "" {
		rpcParams["account"] = c.Account
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  rpcParams,
		"id":      c.nextID.Add(1),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("signal rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return err
	}
	if rpc.Error != nil {
		return rpc.Error
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(rpc.Result, result)
}

func (c *Client) Events(ctx context.Context, handle func(Event) error) error {
	if handle == nil {
		return errors.New("signal event handler required")
	}
	if err := c.validate(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/events", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("signal events status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data strings.Builder
	dispatch := func() error {
		raw := strings.TrimSpace(data.String())
		data.Reset()
		if raw == "" {
			return nil
		}
		ev, err := parseEvent([]byte(raw))
		if err != nil {
			return err
		}
		if err := handle(ev); err != nil {
			return handlerError{err: err}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return dispatch()
}

func (c *Client) TailEvents(ctx context.Context, opts TailOptions, handle func(Event) error) error {
	minBackoff := opts.RetryMin
	if minBackoff <= 0 {
		minBackoff = 250 * time.Millisecond
	}
	maxBackoff := opts.RetryMax
	if maxBackoff <= 0 || maxBackoff < minBackoff {
		maxBackoff = 2 * time.Second
	}
	backoff := minBackoff
	reconnects := 0
	for {
		err := c.Events(ctx, handle)
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

func (c *Client) targetParams(recipients []string, groupID string) map[string]any {
	params := map[string]any{}
	cleanRecipients := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		if recipient = strings.TrimSpace(recipient); recipient != "" {
			cleanRecipients = append(cleanRecipients, recipient)
		}
	}
	if len(cleanRecipients) > 0 {
		params["recipient"] = cleanRecipients
	}
	if strings.TrimSpace(groupID) != "" {
		params["groupId"] = strings.TrimSpace(groupID)
	}
	return params
}

func hasTarget(recipients []string, groupID string) bool {
	if strings.TrimSpace(groupID) != "" {
		return true
	}
	for _, recipient := range recipients {
		if strings.TrimSpace(recipient) != "" {
			return true
		}
	}
	return false
}

func parseEvent(raw []byte) (Event, error) {
	var note struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &note); err != nil {
		return Event{}, err
	}
	var params struct {
		Account      string `json:"account"`
		Subscription *int   `json:"subscription"`
		Envelope     *Envelope
		Result       *struct {
			Account      string `json:"account"`
			Subscription *int   `json:"subscription"`
			Envelope     *Envelope
		} `json:"result"`
	}
	if err := json.Unmarshal(note.Params, &params); err != nil {
		return Event{}, err
	}
	ev := Event{Method: note.Method, Account: params.Account, Subscription: params.Subscription, Raw: append([]byte(nil), raw...)}
	if params.Envelope != nil {
		ev.Envelope = *params.Envelope
		return ev, nil
	}
	if params.Result != nil {
		ev.Account = params.Result.Account
		ev.Subscription = params.Result.Subscription
		if params.Result.Envelope != nil {
			ev.Envelope = *params.Result.Envelope
		}
	}
	return ev, nil
}

func (c *Client) validate() error {
	if c == nil {
		return errors.New("signal client nil")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("signal base URL required")
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
