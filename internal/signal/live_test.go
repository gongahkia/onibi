package signal

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveSignal(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_SIGNAL") != "1" {
		t.Skip("set ONIBI_LIVE_SIGNAL=1")
	}
	envs := []string{"ONIBI_SIGNAL_RPC_URL", "ONIBI_SIGNAL_ACCOUNT", "ONIBI_SIGNAL_RECIPIENT", "ONIBI_SIGNAL_STREAM"}
	rec, err := liveartifact.New("signal", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	baseURL := strings.TrimSpace(os.Getenv("ONIBI_SIGNAL_RPC_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:6001"
	}
	c := New(baseURL, os.Getenv("ONIBI_SIGNAL_ACCOUNT"))
	if err := c.Check(t.Context()); err != nil {
		rec.Error("check", err)
		t.Fatal(err)
	}
	rec.Record("check", map[string]any{"ok": true})
	body := "live signal smoke " + time.Now().UTC().Format(time.RFC3339Nano)
	if recipient := strings.TrimSpace(os.Getenv("ONIBI_SIGNAL_RECIPIENT")); recipient != "" {
		if _, err := c.Send(t.Context(), SendRequest{Recipients: []string{recipient}, Message: body}); err != nil {
			rec.Error("send", err)
			t.Fatal(err)
		}
		rec.Record("send", map[string]any{"ok": true, "message_bytes": len(body)})
	}
	if os.Getenv("ONIBI_SIGNAL_STREAM") == "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		errMatched := errors.New("matched")
		err := c.Events(ctx, func(ev Event) error {
			if ev.Envelope.DataMessage != nil {
				rec.Record("stream", map[string]any{"source": ev.Envelope.Source, "message_bytes": len(ev.Envelope.DataMessage.Message)})
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
