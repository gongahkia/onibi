package telegram

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestLiveTelegram(t *testing.T) {
	token := os.Getenv("ONIBI_LIVE_TELEGRAM_TOKEN")
	chat := os.Getenv("ONIBI_LIVE_TELEGRAM_CHAT_ID")
	if token == "" || chat == "" {
		t.Skip("set ONIBI_LIVE_TELEGRAM_TOKEN and ONIBI_LIVE_TELEGRAM_CHAT_ID")
	}
	chatID, err := strconv.ParseInt(chat, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := NewClient(token)
	if _, err := c.GetMe(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SendMessage(ctx, chatID, "onibi live telegram smoke", nil); err != nil {
		t.Fatal(err)
	}
}
