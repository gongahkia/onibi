// Package cli wires cobra subcommands.
package cli

import (
	"github.com/spf13/cobra"
)

// Root returns the configured top-level command tree.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "onibi",
		Short:         "Telegram-controlled coding-agent host",
		Long:          "Onibi hosts coding agents (Claude Code, Codex, OpenCode, Goose, Gemini, Copilot, Pi, Amp) under PTYs and routes approval prompts, shell events, and turn signals to a Telegram bot for one-handed control from your phone.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		runCmd(),
		wrapCmd(),
		setupCmd(),
		getChatIDCmd(),
		rotateTokenCmd(),
		doctorCmd(),
		configCmd(),
		installHooksCmd(),
		installServiceCmd(),
		uninstallServiceCmd(),
		uninstallCmd(),
		adaptersCmd(),
		sessionsCmd(),
		logCmd(),
		tailLogCmd(),
		versionCmd(),
	)

	return root
}
