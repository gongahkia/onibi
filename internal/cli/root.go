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
		Short:         "Web-controlled coding-agent host",
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
	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Start"},
		&cobra.Group{ID: "control", Title: "Control"},
		&cobra.Group{ID: "integrate", Title: "Integrate"},
		&cobra.Group{ID: "inspect", Title: "Inspect"},
		&cobra.Group{ID: "maintain", Title: "Maintain"},
	)

	addGrouped(root, "start", quickstartCmd(), setupCmd(), upCmd(), pairCmd(), telegramCmd(), logoCmd())
	addGrouped(root, "control", runCmd(), wrapCmd(), newSessionCmd(), showCmd(), hideCmd(), snapshotCmd(), restoreCmd(), forkCmd(), snapshotsCmd(), shellCmd(), demoCmd(), projectCmd())
	addGrouped(root, "integrate", adaptersCmd(), installHooksCmd(), hooksCmd(), discordCmd(), mcpCmd())
	addGrouped(root, "inspect", statusCmd(), devicesCmd(), sessionsCmd(), budgetCmd(), pingCmd(), doctorCmd(), logCmd(), tailLogCmd(), versionCmd())
	addGrouped(root, "maintain", configCmd(), storeCmd(), trustCmd(), unpairCmd(), installServiceCmd(), uninstallServiceCmd(), uninstallCmd(), updateCheckCmd(), supportBundleCmd())

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

func addGrouped(root *cobra.Command, group string, cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.GroupID = group
	}
	root.AddCommand(cmds...)
}
