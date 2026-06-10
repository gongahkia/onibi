package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func BotCommands() []models.BotCommand {
	return []models.BotCommand{
		{Command: "sessions", Description: "list active sessions"},
		{Command: "status", Description: "show daemon status"},
		{Command: "help", Description: "show Onibi commands"},
		{Command: "text", Description: "force text output for a session"},
		{Command: "screenshot", Description: "force screenshot output for a session"},
		{Command: "snooze", Description: "pause notifications"},
		{Command: "new", Description: "start a new agent session"},
		{Command: "log", Description: "show recent audit entries"},
	}
}

func RegisterCommands(ctx context.Context, api API) error {
	if api == nil {
		return nil
	}
	_, err := api.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: BotCommands()})
	return err
}
