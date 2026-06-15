package telegram

import (
	"context"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type CommandSpec struct {
	Name     string
	Args     string
	Short    string
	Detail   string
	Category string
	Examples []string
}

var commandSpecs = []CommandSpec{
	{Name: "/status", Short: "show daemon status", Detail: "Shows daemon uptime, encrypted mode, default target, active session count, pending approval count, queued prompt count, snooze state, and current sessions.", Category: "sessions"},
	{Name: "/sessions", Short: "list active sessions", Detail: "Lists active agent, shell, and tmux sessions, including session ids, names, agent type, age, state, command, and default target marker.", Category: "sessions"},
	{Name: "/menu", Short: "show session actions", Detail: "Shows inline action buttons for status, sessions, queue, secure controls, and live session actions.", Category: "sessions"},
	{Name: "/target", Args: "<id|name>", Short: "set default session", Detail: "Sets the default session for this chat. Without an argument, shows the current default target.", Category: "sessions", Examples: []string{"/target claude", "/target abc123"}},
	{Name: "/new", Args: "<agent|shell|tmux> [args...]", Short: "start an agent, shell, or tmux session", Detail: "Starts a headless local agent or shell session, or attaches to a visible laptop tmux target, then routes future prompts to it.", Category: "sessions", Examples: []string{"/new shell", "/new claude", "/new tmux onibi", "/new tmux %1", "/new codex -- --model gpt-5-codex"}},
	{Name: "/peek", Args: "<id|name>", Short: "send session preview", Detail: "Sends a current preview of the target session output using the configured render mode.", Category: "controls"},
	{Name: "/text", Args: "<id|name>", Short: "force text output", Detail: "Forces future previews for the target session to use text output.", Category: "controls"},
	{Name: "/screenshot", Args: "<id|name>", Short: "force screenshots", Detail: "Forces future previews for the target session to use screenshot output.", Category: "controls"},
	{Name: "/interrupt", Args: "<id|name>", Short: "send Ctrl-C", Detail: "Sends Ctrl-C to the target session and marks it idle. TOTP is required when configured.", Category: "controls"},
	{Name: "/enter", Args: "[id|name]", Short: "send Enter", Detail: "Sends a bare Enter/newline to the target session, useful for TUI confirmation prompts.", Category: "controls", Examples: []string{"/enter", "/enter codex"}},
	{Name: "/kill", Args: "<id|name>", Short: "terminate session", Detail: "Terminates the target session and marks it ended. TOTP is required when configured.", Category: "controls"},
	{Name: "/rename", Args: "<id|name> <name>", Short: "rename session", Detail: "Renames a live session. In encrypted mode, plaintext rename with a new name is refused; use /secure.", Category: "controls"},
	{Name: "/queue", Args: "[id|name]", Short: "list queued prompts", Detail: "Lists queued prompts for a session, or all sessions when no target is supplied.", Category: "prompts"},
	{Name: "/prompt", Args: "<text>", Short: "queue a prompt", Detail: "Queues a prompt to the default target session. If no default is set and multiple sessions are live, a target picker is shown. The prompt is dispatched after the next agent_done signal.", Category: "prompts", Examples: []string{"/prompt write tests for the new field"}},
	{Name: "/send", Args: "<text>", Short: "send text, including leading /", Detail: "Sends text directly to the target session. Use this when the text itself starts with a slash.", Category: "prompts", Examples: []string{"/send /help", "//help"}},
	{Name: "/editprompt", Args: "<id> <text>", Short: "edit a queued prompt", Detail: "Replaces the text of a queued prompt. Sent or cancelled prompts cannot be edited.", Category: "prompts"},
	{Name: "/cancelprompt", Args: "<id>", Short: "cancel a queued prompt", Detail: "Cancels a queued prompt by id.", Category: "prompts"},
	{Name: "/moveprompt", Args: "<id> <position>", Short: "reorder queued prompts", Detail: "Moves a queued prompt to a new queue position.", Category: "prompts"},
	{Name: "/flushqueue", Args: "[id|name]", Short: "cancel queued prompts", Detail: "Cancels queued prompts for a session, or all queued prompts when no target is supplied.", Category: "prompts"},
	{Name: "/secure", Short: "open encrypted controls", Detail: "Opens the encrypted Mini App controls. Use this for prompt entry and approval decisions when encrypted mode is on.", Category: "security"},
	{Name: "/snooze", Args: "[duration|agent [duration]]", Short: "pause non-approval notifications", Detail: "Pauses non-approval notifications globally or for one agent. Approvals still notify.", Category: "notifications", Examples: []string{"/snooze 1h", "/snooze claude 30m"}},
	{Name: "/unsnooze", Args: "[agent]", Short: "resume notifications", Detail: "Resumes snoozed notifications globally or for one agent.", Category: "notifications"},
	{Name: "/log", Args: "[n]", Short: "show recent audit entries", Detail: "Shows recent local audit entries. n defaults to the daemon's configured limit.", Category: "diagnostics"},
	{Name: "/help", Short: "show this help", Detail: "Shows the command list. Use /help <command> for detailed help.", Category: "diagnostics", Examples: []string{"/help prompt", "/help /kill"}},
}

func CommandSpecs() []CommandSpec {
	out := make([]CommandSpec, len(commandSpecs))
	copy(out, commandSpecs)
	return out
}

func BotCommands() []models.BotCommand {
	cmds := make([]models.BotCommand, 0, len(commandSpecs))
	for _, c := range commandSpecs {
		cmds = append(cmds, models.BotCommand{
			Command:     strings.TrimPrefix(c.Name, "/"),
			Description: c.Short,
		})
	}
	return cmds
}

func HelpText() string {
	var b strings.Builder
	b.WriteString("Onibi commands")
	last := ""
	for _, c := range commandSpecs {
		if c.Category != last {
			last = c.Category
			b.WriteString("\n\n")
			b.WriteString(categoryTitle(last))
			b.WriteString(":")
		}
		b.WriteString("\n")
		b.WriteString(c.Line())
	}
	b.WriteString("\n\nUse /help <command> for details.")
	return b.String()
}

func HelpDetail(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return HelpText()
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	for _, c := range commandSpecs {
		if c.Name != name {
			continue
		}
		var b strings.Builder
		b.WriteString(c.Usage())
		b.WriteString("\n\n")
		if c.Detail != "" {
			b.WriteString(c.Detail)
		} else {
			b.WriteString(c.Short)
		}
		if len(c.Examples) > 0 {
			b.WriteString("\n\nExamples:")
			for _, e := range c.Examples {
				b.WriteString("\n  ")
				b.WriteString(e)
			}
		}
		return b.String()
	}
	return "No such command. Try /help"
}

func CommandLinesForReadme() []string {
	lines := make([]string, 0, len(commandSpecs))
	for _, c := range commandSpecs {
		lines = append(lines, c.Line())
	}
	return lines
}

func (c CommandSpec) Usage() string {
	if c.Args == "" {
		return c.Name
	}
	return c.Name + " " + c.Args
}

func (c CommandSpec) Line() string {
	return c.Usage() + " - " + c.Short
}

func categoryTitle(s string) string {
	switch s {
	case "sessions":
		return "Sessions"
	case "controls":
		return "Controls"
	case "prompts":
		return "Prompts"
	case "security":
		return "Security"
	case "notifications":
		return "Notifications"
	case "diagnostics":
		return "Diagnostics"
	default:
		return s
	}
}

func RegisterCommands(ctx context.Context, api API) error {
	if api == nil {
		return nil
	}
	_, err := api.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: BotCommands()})
	return err
}
