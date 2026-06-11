package telegram

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-telegram/bot/models"
)

// ProbeResult is the non-destructive Telegram reachability result.
type ProbeResult struct {
	Self             *models.User
	GetUpdatesOK     bool
	GetUpdatesDetail string
	WebhookURL       string
	WebhookDetail    string
}

// ProbeToken validates token with getMe and probes getUpdates without
// deleting webhooks or confirming updates.
func ProbeToken(ctx context.Context, token string, allowEnvProxy bool) (*ProbeResult, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("telegram: empty token")
	}
	hc := &http.Client{
		Timeout: HTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			Proxy:           noProxy(allowEnvProxy),
		},
	}
	self := &models.User{}
	if err := rawBotCall(ctx, hc, token, "getMe", nil, self); err != nil {
		return nil, err
	}
	res := &ProbeResult{Self: self}
	var webhook struct {
		URL                string `json:"url"`
		PendingUpdateCount int    `json:"pending_update_count"`
	}
	if err := rawBotCall(ctx, hc, token, "getWebhookInfo", map[string]any{}, &webhook); err == nil {
		res.WebhookURL = webhook.URL
		if webhook.URL != "" {
			res.WebhookDetail = fmt.Sprintf("%s (%d pending)", webhook.URL, webhook.PendingUpdateCount)
		}
	}
	var updates []models.Update
	err := rawBotCall(ctx, hc, token, "getUpdates", map[string]any{
		"offset":          0,
		"limit":           1,
		"timeout":         0,
		"allowed_updates": AllowedUpdateTypes,
	}, &updates)
	if err != nil {
		if detail, ok := getUpdatesConflictDetail(err); ok {
			res.GetUpdatesOK = false
			res.GetUpdatesDetail = detail
			return res, nil
		}
		return res, err
	}
	res.GetUpdatesOK = true
	res.GetUpdatesDetail = "ok"
	return res, nil
}

func getUpdatesConflictDetail(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "409") && !strings.Contains(msg, "conflict") {
		return "", false
	}
	if strings.Contains(msg, "webhook") {
		return "conflict: webhook is active; deleteWebhook must succeed before polling", true
	}
	return "conflict: another getUpdates poller is active", true
}

func rawBotCall(ctx context.Context, hc *http.Client, token, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var wire struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return err
	}
	if !wire.OK {
		return fmt.Errorf("telegram %s failed (%d): %s", method, wire.ErrorCode, wire.Description)
	}
	if out != nil {
		return json.Unmarshal(wire.Result, out)
	}
	return nil
}
