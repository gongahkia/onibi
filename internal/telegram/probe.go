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
	var updates []models.Update
	err := rawBotCall(ctx, hc, token, "getUpdates", map[string]any{
		"offset":          0,
		"limit":           1,
		"timeout":         0,
		"allowed_updates": AllowedUpdateTypes,
	}, &updates)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "409") || strings.Contains(strings.ToLower(msg), "conflict") {
			res.GetUpdatesOK = true
			res.GetUpdatesDetail = "reachable; another getUpdates poller is active"
			return res, nil
		}
		return res, err
	}
	res.GetUpdatesOK = true
	res.GetUpdatesDetail = "ok"
	return res, nil
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
