package gotify

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveGotify(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_GOTIFY") != "1" {
		t.Skip("set ONIBI_LIVE_GOTIFY=1")
	}
	envs := []string{"ONIBI_GOTIFY_URL", "ONIBI_GOTIFY_APP_TOKEN", "ONIBI_GOTIFY_CLIENT_TOKEN", "ONIBI_GOTIFY_ACTION_BASE_URL", "ONIBI_GOTIFY_STREAM"}
	rec, err := liveartifact.New("gotify", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c := New(os.Getenv("ONIBI_GOTIFY_URL"), os.Getenv("ONIBI_GOTIFY_APP_TOKEN"), os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN"))
	if err := c.Validate(t.Context()); err != nil {
		rec.Error("validate", err)
		t.Fatal(err)
	}
	rec.Record("validate", map[string]any{"ok": true})
	body := "live gotify smoke " + time.Now().UTC().Format(time.RFC3339Nano)
	msg := Message{Title: "Onibi", Message: body}
	if baseURL := os.Getenv("ONIBI_GOTIFY_ACTION_BASE_URL"); baseURL != "" {
		clickURL := strings.TrimRight(baseURL, "/") + "/gotify/approval/live"
		msg.Message += "\n\nOpen approval: " + clickURL
		msg.Extras = ApprovalExtras(clickURL)
	}
	var streamErr <-chan error
	if os.Getenv("ONIBI_GOTIFY_STREAM") == "1" && os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN") != "" {
		errCh := make(chan error, 1)
		streamErr = errCh
		streamCtx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		errMatched := errors.New("matched")
		go func() {
			err := c.Stream(streamCtx, func(got StreamMessage) error {
				if got.Message == body {
					rec.Record("stream", map[string]any{"id": got.ID, "title": got.Title})
					return errMatched
				}
				return nil
			})
			if errors.Is(err, errMatched) {
				errCh <- nil
				return
			}
			errCh <- err
		}()
		time.Sleep(200 * time.Millisecond)
	}
	if err := c.Send(t.Context(), msg); err != nil {
		rec.Error("send", err)
		t.Fatal(err)
	}
	rec.Record("send", map[string]any{"ok": true, "action_url": msg.Extras != nil})
	if streamErr != nil {
		if err := <-streamErr; err != nil {
			rec.Error("stream", err)
			t.Fatal(err)
		}
	}
}
