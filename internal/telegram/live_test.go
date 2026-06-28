package telegram

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveTelegram(t *testing.T) {
	token := os.Getenv("ONIBI_LIVE_TELEGRAM_TOKEN")
	chat := os.Getenv("ONIBI_LIVE_TELEGRAM_CHAT_ID")
	if token == "" || chat == "" {
		t.Skip("set ONIBI_LIVE_TELEGRAM_TOKEN and ONIBI_LIVE_TELEGRAM_CHAT_ID")
	}
	envs := []string{"ONIBI_LIVE_TELEGRAM_TOKEN", "ONIBI_LIVE_TELEGRAM_CHAT_ID"}
	rec, err := liveartifact.New("telegram", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	chatID, err := strconv.ParseInt(chat, 10, 64)
	if err != nil {
		rec.Error("parse-chat-id", err)
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClient(token)
	if _, err := c.GetMe(ctx); err != nil {
		rec.Error("get-me", err)
		t.Fatal(err)
	}
	rec.Record("get-me", map[string]any{"ok": true})
	if _, err := c.SendMessage(ctx, chatID, "onibi live telegram smoke", nil); err != nil {
		rec.Error("send-message", err)
		t.Fatal(err)
	}
	rec.Record("send-message", map[string]any{"ok": true})
}
