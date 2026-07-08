package apns

import (
	"os"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveAPNs(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_APNS") != "1" {
		t.Skip("set ONIBI_LIVE_APNS=1")
	}
	envs := []string{"ONIBI_APNS_KEY_PATH", "ONIBI_APNS_KEY_ID", "ONIBI_APNS_TEAM_ID", "ONIBI_APNS_TOPIC", "ONIBI_APNS_DEVICE_TOKEN", "ONIBI_APNS_ENV"}
	rec, err := liveartifact.New("apns", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c, err := New(Config{
		KeyPath:     os.Getenv("ONIBI_APNS_KEY_PATH"),
		KeyID:       os.Getenv("ONIBI_APNS_KEY_ID"),
		TeamID:      os.Getenv("ONIBI_APNS_TEAM_ID"),
		Topic:       os.Getenv("ONIBI_APNS_TOPIC"),
		Environment: os.Getenv("ONIBI_APNS_ENV"),
	})
	if err != nil {
		rec.Error("client", err)
		t.Fatal(err)
	}
	result, err := c.PushApproval(t.Context(), PushRequest{
		DeviceToken: os.Getenv("ONIBI_APNS_DEVICE_TOKEN"),
		Title:       "Onibi",
		Body:        "live APNs smoke " + time.Now().UTC().Format(time.RFC3339Nano),
		ApprovalID:  "live",
		CollapseID:  "onibi-live",
		TTL:         30 * time.Second,
	})
	rec.Record("push", map[string]any{"sent": result.Sent, "status": result.StatusCode, "reason": result.Reason, "apns_id": result.APNsID != ""})
	if err != nil {
		t.Fatal(err)
	}
}
