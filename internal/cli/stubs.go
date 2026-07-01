package cli

import (
	"fmt"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [agent [args...]]",
		Short: "Start the daemon (with an optional agent spawn)",
		RunE:  runRun,
	}
	addRunFlags(cmd)
	return cmd
}

func wrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wrap <command> [args...]",
		Short: "Wrap any local TUI/CLI under Onibi PTY control",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runRun,
	}
	addRunFlags(cmd)
	return cmd
}

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [--headless|--visible] <agent|shell> [args...]",
		Short: "Create a tmux-backed Onibi session",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runNewSession,
	}
	cmd.Flags().Bool("headless", false, "start without opening a terminal window")
	cmd.Flags().Bool("visible", false, "start and open a terminal window")
	cmd.Flags().String("name", "", "session label")
	return cmd
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [id|name]",
		Short: "Open a visible terminal for a tmux-backed session",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runShowSession,
	}
}

func hideCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hide [id|name]",
		Short: "Detach visible clients or end a tmux-backed session",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runHideSession,
	}
	cmd.Flags().Bool("headless", false, "detach terminal clients and continue headless")
	cmd.Flags().Bool("end", false, "end the session")
	return cmd
}

func snapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot <session> <name>",
		Short: "Save a session snapshot",
		Args:  cobra.ExactArgs(2),
		RunE:  runSnapshotTake,
	}
}

func restoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <name>",
		Short: "Restore a saved snapshot",
		Args:  cobra.ExactArgs(1),
		RunE:  runSnapshotRestore,
	}
}

func forkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `fork <name> @turn-N "new prompt"`,
		Short: "Fork a snapshot from a transcript turn",
		Args:  cobra.ExactArgs(3),
		RunE:  runSnapshotFork,
	}
}

func snapshotsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "List or delete saved snapshots",
		RunE:  runSnapshotsList,
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List saved snapshots",
		RunE:  runSnapshotsList,
	}
	del := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a saved snapshot",
		Args:    cobra.ExactArgs(1),
		RunE:    runSnapshotDelete,
	}
	cmd.AddCommand(list, del)
	return cmd
}

func recordingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recordings",
		Short: "List, export, or delete session recordings",
		RunE:  runRecordingsList,
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List session recordings",
		RunE:  runRecordingsList,
	}
	export := &cobra.Command{
		Use:   "export <session> <path>",
		Short: "Export a session recording",
		Args:  cobra.ExactArgs(2),
		RunE:  runRecordingsExport,
	}
	del := &cobra.Command{
		Use:     "delete <session>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a session recording",
		Args:    cobra.ExactArgs(1),
		RunE:    runRecordingsDelete,
	}
	cmd.AddCommand(list, export, del)
	return cmd
}

func addRunFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "session label (defaults to agent name)")
	cmd.Flags().Int("buffer", 0, "PTY output buffer size in bytes (default 64 KiB)")
	cmd.Flags().String("attach-tmux", "", "attach an existing tmux target instead of spawning an agent")
	cmd.Flags().Bool("debug", false, "enable debug logs")
	cmd.Flags().String("log-file", "", "daemon log file (default: state log dir/onibi.log)")
	cmd.Flags().String("argv0", "", "internal argv[0] override")
	_ = cmd.Flags().MarkHidden("argv0")
}

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "setup",
		Aliases: []string{"init"},
		Short:   "Show web setup guidance",
		RunE:    runSetup,
	}
	cmd.Flags().Bool("print-checklist", false, "print setup security checklist and exit")
	cmd.Flags().Bool("complete", false, "offer service install, hook install, and doctor")
	return cmd
}

func updateCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update-check",
		Aliases: []string{"check-update"},
		Short:   "Check whether Onibi has an available update",
		RunE:    runUpdateCheck,
	}
	cmd.Flags().String("repo", "", "local Onibi repo path")
	cmd.Flags().Bool("no-github", false, "skip GitHub release check")
	cmd.Flags().Bool("json", false, "print JSON")
	return cmd
}

func demoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run guided local demos",
		RunE:  runDemo,
	}
	cmd.Flags().Duration("duration", 90*time.Second, "scripted demo duration")
	cmd.Flags().Bool("approval", false, "create a local test approval")
	addDemoApprovalFlags(cmd)
	approval := &cobra.Command{
		Hidden: true,
		Use:    "approval",
		Short:  "Create a local test approval",
		RunE:   runDemoApproval,
	}
	addDemoApprovalFlags(approval)
	cmd.AddCommand(approval)
	return cmd
}

func addDemoApprovalFlags(cmd *cobra.Command) {
	cmd.Flags().String("tool", "Bash", "demo tool name")
	cmd.Flags().String("input", `{"command":"echo onibi demo approval"}`, "demo tool input JSON")
}

func projectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage project aliases",
		RunE:  runProject,
	}
	cmd.Flags().Bool("list", false, "list project aliases")
	cmd.Flags().Bool("add", false, "add a project alias")
	cmd.Flags().Bool("forget", false, "forget a project alias")
	list := &cobra.Command{
		Hidden: true,
		Use:    "list",
		Short:  "List project aliases",
		RunE:   runProjectList,
	}
	add := &cobra.Command{
		Hidden: true,
		Use:    "add here | add <alias> <path>",
		Short:  "Add a project alias",
		Args:   cobra.MinimumNArgs(1),
		RunE:   runProjectAdd,
	}
	forget := &cobra.Command{
		Hidden:  true,
		Use:     "forget <alias>",
		Aliases: []string{"remove", "delete", "del"},
		Short:   "Forget a project alias",
		Args:    cobra.ExactArgs(1),
		RunE:    runProjectForget,
	}
	cmd.AddCommand(list, add, forget)
	return cmd
}

func pingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ping",
		Aliases: []string{"health"},
		Short:   "Check daemon socket health",
		RunE:    runPing,
	}
	cmd.Flags().IntP("count", "c", 1, "number of probes")
	cmd.Flags().Duration("interval", time.Second, "delay between probes")
	cmd.Flags().Duration("timeout", 2*time.Second, "per-probe timeout")
	return cmd
}

func doctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"check"},
		Short:   "Run setup + integrity checks",
		RunE:    runDoctor,
	}
	cmd.Flags().Bool("offline", false, "skip live network checks")
	cmd.Flags().String("mode", "auto", "doctor mode (auto, preflight, installed, ci, release)")
	cmd.Flags().String("transport", "", "override transport mode for provider checks")
	cmd.Flags().Bool("fix", false, "apply safe local fixes for doctor warnings")
	cmd.Flags().Bool("after-upgrade", false, "run offline upgrade checks")
	cmd.Flags().Bool("release", false, "run release readiness checks")
	cmd.Flags().Bool("json", false, "print JSON")
	cmd.Flags().Bool("explain", false, "print verbose repair plans")
	return cmd
}

func installHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "install-hooks",
		Aliases: []string{"integrate"},
		Short:   "Install agent and/or shell hooks",
		RunE:    runInstallHooks,
	}
	cmd.Flags().String("agent", "", "agent name (claude, codex, opencode, goose, gemini, copilot, pi, amp)")
	cmd.Flags().String("shell", "", "shell name (zsh, bash, fish)")
	cmd.Flags().Bool("all", false, "install every supported agent adapter")
	cmd.Flags().Bool("interactive", false, "prompt for each detected agent/shell")
	cmd.Flags().Bool("uninstall", false, "remove Onibi-managed hooks for the selected agent/shell")
	return cmd
}

func hooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hooks",
		Aliases: []string{"inspect-hooks"},
		Short:   "Inspect installed hooks",
		RunE:    runHooks,
	}
	cmd.Flags().Bool("show", false, "show hook config, records, backups, and drift")
	cmd.Flags().Bool("matrix", false, "show hook compatibility matrix")
	cmd.Flags().String("agent", "", "agent name")
	cmd.Flags().String("shell", "", "shell name")
	cmd.Flags().Bool("all", false, "show every supported agent and shell hook with --show")
	cmd.Flags().Bool("json", false, "print JSON")
	show := &cobra.Command{
		Hidden: true,
		Use:    "show",
		Short:  "Show hook config, records, backups, and drift",
		RunE:   runHooksShow,
	}
	show.Flags().String("agent", "", "agent name")
	show.Flags().String("shell", "", "shell name")
	show.Flags().Bool("all", false, "show every supported agent and shell hook")
	show.Flags().Bool("json", false, "print JSON")
	matrix := &cobra.Command{
		Hidden: true,
		Use:    "matrix",
		Short:  "Show hook compatibility matrix",
		RunE:   runHooksMatrix,
	}
	matrix.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(show, matrix)
	return cmd
}

func installServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-service",
		Short: "Install and start LaunchAgent (macOS) or systemd user unit (Linux)",
		RunE:  runInstallService,
	}
}

func uninstallServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-service",
		Short: "Stop and remove LaunchAgent / systemd user unit",
		RunE:  runUninstallService,
	}
}

func adaptersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "adapters",
		Aliases: []string{"integrations"},
		Short:   "Show agent and shell adapter status",
		RunE:    runAdapters,
	}
	cmd.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(adaptersAddCmd())
	cmd.AddCommand(adaptersValidateCmd())
	return cmd
}

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sessions",
		Aliases: []string{"ps"},
		Short:   "List sessions recorded by the daemon",
		RunE:    runSessions,
	}
	cmd.Flags().Bool("all", false, "include ended sessions")
	cmd.Flags().Int("n", 50, "number of sessions to print")
	return cmd
}

func logCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Print recent audit-log entries",
		RunE:  runLog,
	}
	cmd.Flags().Int("n", 50, "number of entries to print")
	cmd.Flags().Bool("json", false, "emit one audit entry per line as JSON")
	cmd.Flags().Bool("notify", false, "show notify-only audit entries")
	cmd.Flags().String("export", "", "export full log to file (csv|json by extension)")
	return cmd
}

func tailLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tail-log",
		Short: "Print daemon log tail",
		RunE:  runTailLog,
	}
	cmd.Flags().Int("n", 80, "number of lines to print")
	cmd.Flags().Bool("follow", false, "continue printing appended lines")
	return cmd
}

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the Onibi MCP server on stdio",
		RunE:  runMCP,
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build info",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(fmt.Sprintf("onibi %s", buildinfo.Version))
			cmd.Println("commit " + buildinfo.Commit)
			cmd.Println("date " + buildinfo.Date)
		},
	}
}
