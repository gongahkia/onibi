// Package notifier sends explicitly configured watch events to external targets.
package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/domain"
)

type Sender interface {
	Send(context.Context, domain.Event, domain.NotificationTarget) (int, error)
}

type Dispatcher struct {
	config config.Config
	client *http.Client
}

func New(cfg config.Config) *Dispatcher {
	return &Dispatcher{config: cfg, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (dispatcher *Dispatcher) Send(ctx context.Context, event domain.Event, target domain.NotificationTarget) (int, error) {
	switch target.Kind {
	case domain.NotificationTelegram:
		return dispatcher.sendTelegram(ctx, event, target)
	case domain.NotificationWebhook:
		return dispatcher.sendWebhook(ctx, event, target)
	default:
		return 0, fmt.Errorf("unsupported notification target %q", target.Kind)
	}
}

func (dispatcher *Dispatcher) sendTelegram(ctx context.Context, event domain.Event, target domain.NotificationTarget) (int, error) {
	if !dispatcher.config.Telegram.Enabled {
		return 0, errors.New("Telegram is disabled")
	}
	if target.ChatID == 0 || !containsChatID(dispatcher.config.Telegram.AllowedChatIDs, target.ChatID) {
		return 0, errors.New("Telegram chat ID is not allowlisted")
	}
	token, err := dispatcher.config.ResolveSecret(dispatcher.config.Telegram.BotToken)
	if err != nil || token == "" {
		return 0, errors.New("Telegram bot token is unavailable")
	}
	body, err := json.Marshal(map[string]any{"chat_id": target.ChatID, "text": telegramText(event), "disable_web_page_preview": true})
	if err != nil {
		return 0, fmt.Errorf("encode Telegram event: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+url.PathEscape(token)+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("create Telegram request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "courtsg/0.1")
	return dispatcher.do(request)
}

func (dispatcher *Dispatcher) sendWebhook(ctx context.Context, event domain.Event, target domain.NotificationTarget) (int, error) {
	webhook, ok := dispatcher.config.Webhooks[target.WebhookName]
	if !ok || !webhook.Enabled {
		return 0, fmt.Errorf("webhook %q is not enabled", target.WebhookName)
	}
	endpoint, err := url.Parse(webhook.URL)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return 0, fmt.Errorf("webhook %q has an invalid URL", target.WebhookName)
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname())) {
		return 0, errors.New("webhook URL must use HTTPS unless it is loopback")
	}
	body, err := json.Marshal(map[string]any{"schema_version": "v1", "event": event})
	if err != nil {
		return 0, fmt.Errorf("encode webhook event: %w", err)
	}
	secret, err := dispatcher.config.ResolveSecret(webhook.Secret)
	if err != nil {
		return 0, fmt.Errorf("resolve webhook %q secret: %w", target.WebhookName, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create webhook %q request: %w", target.WebhookName, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "courtsg/0.1")
	request.Header.Set("X-CourtSG-Event-ID", event.ID)
	request.Header.Set("X-CourtSG-Event-Type", event.Type)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		request.Header.Set("X-CourtSG-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	return dispatcher.do(request)
}

func (dispatcher *Dispatcher) do(request *http.Request) (int, error) {
	response, err := dispatcher.client.Do(request)
	if err != nil {
		return 0, errors.New("send notification request")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response.StatusCode, fmt.Errorf("notification endpoint returned HTTP %d", response.StatusCode)
	}
	return response.StatusCode, nil
}

func telegramText(event domain.Event) string {
	return fmt.Sprintf("courtSG %s\nevent: %s\nwatch: %s", event.Type, event.ID, event.WatchID)
}

func containsChatID(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
