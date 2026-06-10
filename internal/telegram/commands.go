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
		{Command: "target", Description: "set default session"},
		{Command: "new", Description: "start an agent session"},
		{Command: "queue", Description: "show queued prompts"},
		{Command: "prompt", Description: "queue a prompt"},
		{Command: "editprompt", Description: "edit a queued prompt"},
		{Command: "cancelprompt", Description: "cancel a queued prompt"},
		{Command: "moveprompt", Description: "reorder queued prompts"},
		{Command: "flushqueue", Description: "cancel queued prompts"},
		{Command: "peek", Description: "send session preview"},
		{Command: "interrupt", Description: "send Ctrl-C to session"},
		{Command: "kill", Description: "terminate a session"},
		{Command: "rename", Description: "rename a session"},
		{Command: "menu", Description: "session action menu"},
		{Command: "snooze", Description: "pause notifications"},
		{Command: "unsnooze", Description: "resume notifications"},
		{Command: "log", Description: "show recent audit entries"},
		{Command: "help", Description: "show Onibi commands"},
		{Command: "text", Description: "force text output for a session"},
		{Command: "screenshot", Description: "force screenshot output for a session"},
	}
}

func RegisterCommands(ctx context.Context, api API) error {
	if api == nil {
		return nil
	}
	_, err := api.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: BotCommands()})
	return err
}
