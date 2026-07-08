package apns

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	apns2 "github.com/sideshow/apns2"
)

func TestPushShape(t *testing.T) {
	sender := &fakeSender{resp: &apns2.Response{StatusCode: http.StatusOK, ApnsID: "apns-1"}}
	c, err := NewWithSender("com.example.onibi", sender)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.PushApproval(t.Context(), PushRequest{
		DeviceToken: "abc123",
		Title:       "Onibi approval",
		Body:        "claude requests Bash",
		ApprovalID:  "a1",
		URL:         "https://onibi.example/approval/a1",
		CollapseID:  "onibi-a1",
		TTL:         30 * time.Second,
		Custom:      map[string]any{"session_id": "s1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Sent || got.APNsID != "apns-1" {
		t.Fatalf("result = %#v", got)
	}
	n := sender.notification
	if n == nil {
		t.Fatal("missing notification")
	}
	if n.DeviceToken != "abc123" || n.Topic != "com.example.onibi" || n.PushType != apns2.PushTypeAlert || n.Priority != apns2.PriorityHigh || n.CollapseID != "onibi-a1" {
		t.Fatalf("notification = %#v", n)
	}
	if n.Expiration.IsZero() {
		t.Fatal("expiration not set")
	}
	var payload map[string]any
	body, ok := n.Payload.([]byte)
	if !ok {
		t.Fatalf("payload type = %T", n.Payload)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	aps := payload["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["title"] != "Onibi approval" || alert["body"] != "claude requests Bash" || aps["sound"] != "default" {
		t.Fatalf("aps = %#v", aps)
	}
	if payload["kind"] != "approval" || payload["approval_id"] != "a1" || payload["url"] != "https://onibi.example/approval/a1" || payload["session_id"] != "s1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestPushRejectsBadShape(t *testing.T) {
	c, err := NewWithSender("com.example.onibi", &fakeSender{resp: &apns2.Response{StatusCode: http.StatusOK}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PushApproval(t.Context(), PushRequest{DeviceToken: "", Title: "t", Body: "b"}); err == nil || !strings.Contains(err.Error(), "device token") {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.PushApproval(t.Context(), PushRequest{DeviceToken: "abc", Title: "t", Body: "b", Custom: map[string]any{"aps": "bad"}}); err == nil || !strings.Contains(err.Error(), "override aps") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushSurfacesAPNsRejection(t *testing.T) {
	c, err := NewWithSender("com.example.onibi", &fakeSender{resp: &apns2.Response{StatusCode: http.StatusBadRequest, Reason: apns2.ReasonBadDeviceToken, ApnsID: "apns-2"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.PushApproval(t.Context(), PushRequest{DeviceToken: "abc123", Title: "Onibi", Body: "approval"})
	var delivery *DeliveryError
	if !errors.As(err, &delivery) || delivery.Result.Reason != apns2.ReasonBadDeviceToken {
		t.Fatalf("err = %v", err)
	}
	if got.Sent || got.StatusCode != http.StatusBadRequest || got.APNsID != "apns-2" {
		t.Fatalf("result = %#v", got)
	}
}

func TestConfigValidation(t *testing.T) {
	if err := (Config{KeyPath: "AuthKey_ABC123DEFG.p8", KeyID: "ABC123DEFG", TeamID: "TEAM123456", Topic: "com.example.onibi"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{KeyPath: "k.p8", KeyID: "kid", TeamID: "team", Topic: "topic", Environment: "bogus"}).Validate(); err == nil {
		t.Fatal("accepted bad environment")
	}
}

type fakeSender struct {
	notification *apns2.Notification
	resp         *apns2.Response
	err          error
}

func (f *fakeSender) PushWithContext(_ apns2.Context, n *apns2.Notification) (*apns2.Response, error) {
	f.notification = n
	return f.resp, f.err
}
