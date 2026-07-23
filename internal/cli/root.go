// Package cli wires cobra subcommands.
package cli

import (
	"strings"

	"github.com/gongahkia/onibi/internal/brand"
	"github.com/spf13/cobra"
)

const rootLong = "Onibi hosts coding agents (Claude Code, Codex, OpenCode, Goose, Gemini, Copilot, Pi, Amp) under PTYs and routes terminal I/O plus approval prompts to a local web cockpit for one-handed control from your phone."

// Root returns the configured top-level command tree.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "onibi",
		Short:         "Your coding-agent cockpit",
		Long:          rootLong,
		RunE:          runRootLanding,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("color", "auto", "color output: auto, always, never")
	root.PersistentFlags().Bool("quiet", false, "suppress banners and nonessential guidance")
	root.PersistentFlags().Bool("debug", false, "enable debug logging and extra error context")
	root.PersistentFlags().Bool("no-logo", false, "suppress ASCII logo in human output")
	root.PersistentFlags().Int("logo-width", 0, "ASCII logo width; default adapts to terminal size")
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == root {
			old := cmd.Long
			cmd.Long = rootHelpLong(cmd)
			defaultHelp(cmd, args)
			cmd.Long = old
			return
		}
		defaultHelp(cmd, args)
	})
	root.AddCommand(startCmd(), phoneCmd(), sessionCmd(), agentCmd(), telegramCmd(), workspaceCmd(), transportCmd(), systemCmd(), demoCmd(), completionCmd(), versionCmd())

	return root
}

func DebugEnabled(cmd *cobra.Command) bool {
	return debug(cmd)
}

func rootHelpLong(cmd *cobra.Command) string {
	if !showLogo(cmd) {
		return rootLong
	}
	return strings.TrimRight(brand.ANSIForWriterWidth(cmd.OutOrStdout(), logoWidth(cmd)), "\n") + "\n\n" + rootLong
}
