// Package cli wires cobra subcommands.
//
// Phase 0: stubs only. Phase 1 implements run/setup/get-chat-id/rotate-token.
// Phase 8 adds install-hooks/install-service/sessions/log.
package cli

import (
	"github.com/spf13/cobra"
)

// Root returns the configured top-level command tree.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "onibi",
		Short:         "Telegram-controlled coding-agent host",
		Long:          "Onibi hosts coding agents (Claude Code, Codex, OpenCode, Goose) under PTYs and routes approval prompts and turn signals to a Telegram bot for one-handed control from your phone.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		runCmd(),
		setupCmd(),
		getChatIDCmd(),
		rotateTokenCmd(),
		doctorCmd(),
		installHooksCmd(),
		installServiceCmd(),
		uninstallServiceCmd(),
		sessionsCmd(),
		logCmd(),
		versionCmd(),
	)

	return root
}
