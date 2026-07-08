package zulip

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveZulip(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_ZULIP") != "1" {
		t.Skip("set ONIBI_LIVE_ZULIP=1")
	}
	envs := []string{"ONIBI_ZULIP_URL", "ONIBI_ZULIP_EMAIL", "ONIBI_ZULIP_API_KEY", "ONIBI_ZULIP_STREAM", "ONIBI_ZULIP_TOPIC_PREFIX", "ONIBI_ZULIP_TAIL"}
	rec, err := liveartifact.New("zulip", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	stream := strings.TrimSpace(os.Getenv("ONIBI_ZULIP_STREAM"))
	if stream == "" {
		t.Fatal("ONIBI_ZULIP_STREAM required")
	}
	topicPrefix := strings.TrimSpace(os.Getenv("ONIBI_ZULIP_TOPIC_PREFIX"))
	if topicPrefix == "" {
		topicPrefix = "onibi-"
	}
	topic := topicPrefix + "live"
	c := New(os.Getenv("ONIBI_ZULIP_URL"), os.Getenv("ONIBI_ZULIP_EMAIL"), os.Getenv("ONIBI_ZULIP_API_KEY"))
	body := "live zulip smoke " + time.Now().UTC().Format(time.RFC3339Nano)
	resp, err := c.SendStreamMessage(t.Context(), StreamMessage{Stream: stream, Topic: topic, Content: body})
	if err != nil {
		rec.Error("send", err)
		t.Fatal(err)
	}
	rec.Record("send", map[string]any{"message_id": resp.ID != 0, "topic": topic})
	if os.Getenv("ONIBI_ZULIP_TAIL") == "1" {
		errMatched := errors.New("matched")
		err := c.TailEvents(t.Context(), TailOptions{
			QueueOptions: QueueOptions{EventTypes: []string{"message"}, Narrow: [][]string{{"channel", stream}}},
			RetryMin:     time.Second,
			RetryMax:     time.Second,
		}, func(ev Event) error {
			if ev.Message != nil && ev.Message.Topic() == topic && ev.Message.Content == body {
				rec.Record("tail", map[string]any{"event_id": ev.ID, "message_id": ev.Message.ID != 0})
				return errMatched
			}
			return nil
		})
		if !errors.Is(err, errMatched) {
			rec.Error("tail", err)
			t.Fatal(err)
		}
	}
}
