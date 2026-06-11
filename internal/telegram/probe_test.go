package telegram

import (
	"errors"
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
