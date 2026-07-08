package ntfy

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveNtfy(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_NTFY") != "1" {
		t.Skip("set ONIBI_LIVE_NTFY=1")
	}
	envs := []string{"ONIBI_NTFY_BASE_URL", "ONIBI_NTFY_TOPIC", "ONIBI_NTFY_TOKEN", "ONIBI_NTFY_ACTION_BASE_URL", "ONIBI_NTFY_STREAM"}
	rec, err := liveartifact.New("ntfy", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c := New(os.Getenv("ONIBI_NTFY_BASE_URL"), os.Getenv("ONIBI_NTFY_TOPIC"), os.Getenv("ONIBI_NTFY_TOKEN"))
	body := "live ntfy smoke " + time.Now().UTC().Format(time.RFC3339Nano)
	msg := Message{Title: "Onibi", Body: body}
	if baseURL := os.Getenv("ONIBI_NTFY_ACTION_BASE_URL"); baseURL != "" {
		baseURL = strings.TrimRight(baseURL, "/")
		msg.Actions = []Action{
			{Type: "http", Label: "Approve", URL: baseURL + "/ntfy/approval/live/approve", Method: "POST", Clear: true},
			{Type: "http", Label: "Deny", URL: baseURL + "/ntfy/approval/live/deny", Method: "POST", Clear: true},
		}
	}
	if err := c.Publish(t.Context(), msg); err != nil {
		rec.Error("publish", err)
		t.Fatal(err)
	}
	rec.Record("publish", map[string]any{"ok": true, "actions": len(msg.Actions)})
	if os.Getenv("ONIBI_NTFY_STREAM") == "1" {
		streamCtx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		errMatched := errors.New("matched")
		err := c.StreamJSON(streamCtx, "1m", func(got StreamMessage) error {
			if got.Message == body {
				rec.Record("stream", map[string]any{"id": got.ID != "", "topic": got.Topic})
				return errMatched
			}
			return nil
		})
		if !errors.Is(err, errMatched) {
			rec.Error("stream", err)
			t.Fatal(err)
		}
	}
}
