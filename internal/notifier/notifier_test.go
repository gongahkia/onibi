package notifier

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
)

func TestWebhookSignsVersionedEvent(t *testing.T) {
	const secret = "test-secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Kaypoh-Event-ID") != "event" || request.Header.Get("X-Kaypoh-Event-Type") != "availability_match" {
			t.Fatalf("missing event headers")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		if request.Header.Get("X-Kaypoh-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Fatal("invalid webhook signature")
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	dispatcher := New(config.Config{Webhooks: map[string]config.Webhook{"test": {Enabled: true, URL: server.URL, Secret: secret}}})
	status, err := dispatcher.Send(context.Background(), domain.Event{ID: "event", WatchID: "watch", Type: domain.WatchTriggerAvailabilityMatch, Payload: []byte(`{"result":"slot"}`)}, domain.NotificationTarget{Kind: domain.NotificationWebhook, WebhookName: "test"})
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("Send() = %d, %v", status, err)
	}
}
