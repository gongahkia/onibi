package setup

import (
	"context"
	"fmt"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/telegram"
)

// RunGetChatID starts a temporary long-poll, prints the first incoming
// chat id, and returns. Fallback for users who can't use deeplinks.
func RunGetChatID(ctx context.Context, token string, io IO) error {
	done := make(chan int64, 1)
	handler := func(_ context.Context, _ *tgbot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.From == nil {
			return
		}
		done <- update.Message.From.ID
	}
	cli, err := telegram.New(ctx, telegram.Options{Token: token, DefaultHandler: handler})
	if err != nil {
		return err
	}
	go cli.Start(ctx)
	select {
	case id := <-done:
		fmt.Fprintf(io.Out, "%d\n", id)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
