package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestGetUpdatesConflictDetailWebhook(t *testing.T) {
	detail, ok := getUpdatesConflictDetail(errors.New("telegram getUpdates failed (409): Conflict: can't use getUpdates method while webhook is active"))
	if !ok {
		t.Fatal("conflict not detected")
	}
	if detail != "conflict: webhook is active; deleteWebhook must succeed before polling" {
		t.Fatalf("detail = %q", detail)
	}
}

func TestGetUpdatesConflictDetailOtherPoller(t *testing.T) {
	detail, ok := getUpdatesConflictDetail(errors.New("telegram getUpdates failed (409): Conflict: terminated by other getUpdates request"))
	if !ok {
		t.Fatal("conflict not detected")
	}
	if detail != "conflict: another getUpdates poller is active" {
		t.Fatalf("detail = %q", detail)
	}
}

func TestRawBotCallRedactsTokenFromTransportError(t *testing.T) {
	token := "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Post %q failed", req.URL.String())
	})}
	err := rawBotCall(context.Background(), hc, token, "getMe", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("redaction missing: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
