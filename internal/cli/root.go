// Package cli wires cobra subcommands.
package cli

import (
	"github.com/gongahkia/onibi/internal/brand"
	"github.com/spf13/cobra"
)

// Root returns the configured top-level command tree.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "onibi",
		Short:         "Web-controlled coding-agent host",
		Long:          brand.ANSI() + "\n\nOnibi hosts coding agents (Claude Code, Codex, OpenCode, Goose, Gemini, Copilot, Pi, Amp) under PTYs and routes terminal I/O plus approval prompts to a local web cockpit for one-handed control from your phone.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("color", "auto", "color output: auto, always, never")

	root.AddCommand(
		runCmd(),
		wrapCmd(),
		newSessionCmd(),
		showCmd(),
		hideCmd(),
		upCmd(),
		shellCmd(),
		setupCmd(),
		getChatIDCmd(),
		rotateTokenCmd(),
		updateCheckCmd(),
		demoCmd(),
		projectCmd(),
		pingCmd(),
		doctorCmd(),
		configCmd(),
		installHooksCmd(),
		hooksCmd(),
		installServiceCmd(),
		uninstallServiceCmd(),
		uninstallCmd(),
		adaptersCmd(),
		sessionsCmd(),
		logCmd(),
		tailLogCmd(),
		supportBundleCmd(),
		mcpCmd(),
		versionCmd(),
	)

	return root
}
