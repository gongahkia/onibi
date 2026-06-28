package pushover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.pushover.net/1"

type Client struct {
	Token   string
	UserKey string
	BaseURL string
	HTTP    *http.Client
}

type MessageOptions struct {
	Title      string
	Message    string
	Priority   int
	Retry      time.Duration
	Expire     time.Duration
	URL        string
	URLTitle   string
}

type MessageResponse struct {
	Status  int      `json:"status"`
	Request string   `json:"request"`
	Receipt string   `json:"receipt"`
	Errors  []string `json:"errors"`
}

type Receipt struct {
	Status       int      `json:"status"`
	Acknowledged int     `json:"acknowledged"`
	AcknowledgedAt int64 `json:"acknowledged_at"`
	Expired      int     `json:"expired"`
	Errors       []string `json:"errors"`
}

func New(token, userKey string) *Client {
	return &Client{Token: strings.TrimSpace(token), UserKey: strings.TrimSpace(userKey), BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Send(ctx context.Context, opts MessageOptions) (MessageResponse, error) {
	if strings.TrimSpace(opts.Message) == "" {
		return MessageResponse{}, errors.New("pushover message required")
	}
	values := url.Values{
		"token":   {c.Token},
		"user":    {c.UserKey},
		"message": {opts.Message},
	}
	if opts.Title != "" {
		values.Set("title", opts.Title)
	}
	if opts.Priority != 0 {
		values.Set("priority", fmt.Sprintf("%d", opts.Priority))
	}
	if opts.Priority == 2 {
		retry := opts.Retry
		if retry <= 0 {
			retry = 30 * time.Second
		}
		expire := opts.Expire
		if expire <= 0 {
			expire = time.Hour
		}
		values.Set("retry", fmt.Sprintf("%d", int(retry.Seconds())))
		values.Set("expire", fmt.Sprintf("%d", int(expire.Seconds())))
	}
	if opts.URL != "" {
		values.Set("url", opts.URL)
	}
	if opts.URLTitle != "" {
		values.Set("url_title", opts.URLTitle)
	}
	var out MessageResponse
	if err := c.postForm(ctx, "/messages.json", values, &out); err != nil {
		return MessageResponse{}, err
	}
	if out.Status != 1 {
		return out, fmt.Errorf("pushover send failed: %s", strings.Join(out.Errors, "; "))
	}
	return out, nil
}

func (c *Client) Receipt(ctx context.Context, receipt string) (Receipt, error) {
	if strings.TrimSpace(receipt) == "" {
		return Receipt{}, errors.New("pushover receipt required")
	}
	values := url.Values{"token": {c.Token}}
	var out Receipt
	if err := c.postForm(ctx, "/receipts/"+url.PathEscape(receipt)+".json", values, &out); err != nil {
		return Receipt{}, err
	}
	if out.Status != 1 {
		return out, fmt.Errorf("pushover receipt failed: %s", strings.Join(out.Errors, "; "))
	}
	return out, nil
}

func (c *Client) PollReceipt(ctx context.Context, receipt string, interval time.Duration) (Receipt, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		got, err := c.Receipt(ctx, receipt)
		if err != nil {
			return got, err
		}
		if got.Acknowledged == 1 || got.Expired == 1 {
			return got, nil
		}
		select {
		case <-ctx.Done():
			return got, ctx.Err()
		case <-tick.C:
		}
	}
}

func (c *Client) postForm(ctx context.Context, p string, values url.Values, dst any) error {
	if c == nil {
		return errors.New("pushover client nil")
	}
	if c.Token == "" || c.UserKey == "" && p == "/messages.json" {
		return errors.New("pushover token/user required")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+p, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("pushover status %d", resp.StatusCode)
	}
	return nil
}
