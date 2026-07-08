package apns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	apns2 "github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
)

const (
	EnvironmentProduction  = "production"
	EnvironmentDevelopment = "development"
	MaxPayloadBytes        = 4096
)

type Config struct {
	KeyPath     string
	KeyID       string
	TeamID      string
	Topic       string
	Environment string
}

type Client struct {
	Topic  string
	Sender Sender
}

type Sender interface {
	PushWithContext(apns2.Context, *apns2.Notification) (*apns2.Response, error)
}

type PushRequest struct {
	DeviceToken string
	Title       string
	Body        string
	ApprovalID  string
	URL         string
	CollapseID  string
	TTL         time.Duration
	Custom      map[string]any
}

type PushResult struct {
	StatusCode int
	APNsID     string
	Reason     string
	Sent       bool
}

type DeliveryError struct {
	Result PushResult
}

func (e *DeliveryError) Error() string {
	if e == nil {
		return ""
	}
	if e.Result.Reason != "" {
		return fmt.Sprintf("apns rejected push: status=%d reason=%s", e.Result.StatusCode, e.Result.Reason)
	}
	return fmt.Sprintf("apns rejected push: status=%d", e.Result.StatusCode)
}

func New(cfg Config) (*Client, error) {
	cfg = normalizeConfig(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	authKey, err := token.AuthKeyFromFile(cfg.KeyPath)
	if err != nil {
		return nil, err
	}
	tok := &token.Token{AuthKey: authKey, KeyID: cfg.KeyID, TeamID: cfg.TeamID}
	sender := apns2.NewTokenClient(tok)
	if cfg.Environment == EnvironmentDevelopment {
		sender = sender.Development()
	} else {
		sender = sender.Production()
	}
	return NewWithSender(cfg.Topic, sender)
}

func NewWithSender(topic string, sender Sender) (*Client, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, errors.New("apns topic required")
	}
	if sender == nil {
		return nil, errors.New("apns sender required")
	}
	return &Client{Topic: topic, Sender: sender}, nil
}

func (cfg Config) Validate() error {
	cfg = normalizeConfig(cfg)
	var missing []string
	if cfg.KeyPath == "" {
		missing = append(missing, "key path")
	}
	if cfg.KeyID == "" {
		missing = append(missing, "key id")
	}
	if cfg.TeamID == "" {
		missing = append(missing, "team id")
	}
	if cfg.Topic == "" {
		missing = append(missing, "topic")
	}
	if len(missing) > 0 {
		return fmt.Errorf("apns missing %s", strings.Join(missing, ", "))
	}
	switch cfg.Environment {
	case EnvironmentProduction, EnvironmentDevelopment:
		return nil
	default:
		return fmt.Errorf("apns environment must be %q or %q", EnvironmentProduction, EnvironmentDevelopment)
	}
}

func (c *Client) PushApproval(ctx context.Context, req PushRequest) (PushResult, error) {
	if c == nil || c.Sender == nil {
		return PushResult{}, errors.New("apns client required")
	}
	req = normalizePushRequest(req)
	if req.DeviceToken == "" {
		return PushResult{}, errors.New("apns device token required")
	}
	if req.Title == "" || req.Body == "" {
		return PushResult{}, errors.New("apns title and body required")
	}
	body, err := approvalPayload(req)
	if err != nil {
		return PushResult{}, err
	}
	n := &apns2.Notification{
		DeviceToken: req.DeviceToken,
		Topic:       c.Topic,
		Payload:     body,
		PushType:    apns2.PushTypeAlert,
		Priority:    apns2.PriorityHigh,
		CollapseID:  req.CollapseID,
	}
	if req.TTL > 0 {
		n.Expiration = time.Now().Add(req.TTL)
	}
	resp, err := c.Sender.PushWithContext(ctx, n)
	if err != nil {
		return PushResult{}, err
	}
	if resp == nil {
		return PushResult{}, errors.New("apns empty response")
	}
	result := PushResult{StatusCode: resp.StatusCode, APNsID: resp.ApnsID, Reason: resp.Reason, Sent: resp.Sent()}
	if !result.Sent {
		return result, &DeliveryError{Result: result}
	}
	return result, nil
}

func approvalPayload(req PushRequest) ([]byte, error) {
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{
				"title": req.Title,
				"body":  req.Body,
			},
			"sound": "default",
		},
		"kind": "approval",
	}
	if req.ApprovalID != "" {
		payload["approval_id"] = req.ApprovalID
	}
	if req.URL != "" {
		payload["url"] = req.URL
	}
	for k, v := range req.Custom {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if k == "aps" {
			return nil, errors.New("apns custom payload cannot override aps")
		}
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(body) > MaxPayloadBytes {
		return nil, fmt.Errorf("apns payload exceeds %d bytes", MaxPayloadBytes)
	}
	return body, nil
}

func normalizeConfig(cfg Config) Config {
	cfg.KeyPath = strings.TrimSpace(cfg.KeyPath)
	cfg.KeyID = strings.TrimSpace(cfg.KeyID)
	cfg.TeamID = strings.TrimSpace(cfg.TeamID)
	cfg.Topic = strings.TrimSpace(cfg.Topic)
	cfg.Environment = strings.ToLower(strings.TrimSpace(cfg.Environment))
	if cfg.Environment == "" {
		cfg.Environment = EnvironmentProduction
	}
	return cfg
}

func normalizePushRequest(req PushRequest) PushRequest {
	req.DeviceToken = strings.TrimSpace(req.DeviceToken)
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)
	req.ApprovalID = strings.TrimSpace(req.ApprovalID)
	req.URL = strings.TrimSpace(req.URL)
	req.CollapseID = strings.TrimSpace(req.CollapseID)
	return req
}
