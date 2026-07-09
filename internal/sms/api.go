package sms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.twilio.com"

type Client struct {
	AccountSID          string
	AuthToken           string
	From                string
	MessagingServiceSID string
	BaseURL             string
	HTTP                *http.Client
}

type Message struct {
	To   string
	Body string
}

type MessageResponse struct {
	SID          string `json:"sid"`
	Status       string `json:"status"`
	ErrorCode    *int   `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type APIError struct {
	StatusCode int
	Code       int
	Message    string
}

func (e APIError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Code != 0 {
		return fmt.Sprintf("twilio status=%d code=%d: %s", e.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("twilio status=%d: %s", e.StatusCode, msg)
}

func New(accountSID, authToken, from, messagingServiceSID string) *Client {
	return &Client{AccountSID: accountSID, AuthToken: authToken, From: from, MessagingServiceSID: messagingServiceSID, BaseURL: DefaultBaseURL}
}

func (c *Client) Send(ctx context.Context, msg Message) (MessageResponse, error) {
	if c == nil {
		return MessageResponse{}, errors.New("sms client nil")
	}
	accountSID := strings.TrimSpace(c.AccountSID)
	authToken := strings.TrimSpace(c.AuthToken)
	to := strings.TrimSpace(msg.To)
	body := strings.TrimSpace(msg.Body)
	from := strings.TrimSpace(c.From)
	messagingServiceSID := strings.TrimSpace(c.MessagingServiceSID)
	switch {
	case accountSID == "":
		return MessageResponse{}, errors.New("twilio account sid required")
	case authToken == "":
		return MessageResponse{}, errors.New("twilio auth token required")
	case to == "":
		return MessageResponse{}, errors.New("sms recipient required")
	case body == "":
		return MessageResponse{}, errors.New("sms body required")
	case from == "" && messagingServiceSID == "":
		return MessageResponse{}, errors.New("twilio from or messaging service sid required")
	}
	values := url.Values{"To": {to}, "Body": {body}}
	if messagingServiceSID != "" {
		values.Set("MessagingServiceSid", messagingServiceSID)
	} else {
		values.Set("From", from)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	reqURL := baseURL + "/2010-04-01/Accounts/" + url.PathEscape(accountSID) + "/Messages.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(values.Encode()))
	if err != nil {
		return MessageResponse{}, err
	}
	req.SetBasicAuth(accountSID, authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return MessageResponse{}, err
	}
	defer resp.Body.Close()
	var out MessageResponse
	var apiErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MessageResponse{}, err
	}
	_ = json.Unmarshal(data, &out)
	_ = json.Unmarshal(data, &apiErr)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return out, APIError{StatusCode: resp.StatusCode, Code: apiErr.Code, Message: apiErr.Message}
	}
	return out, nil
}
