package cli

import (
	"fmt"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [agent [args...]]",
		Short: "Start the daemon (with an optional agent spawn)",
		RunE:  runRun,
	}
	cmd.Flags().String("name", "", "session label (defaults to agent name)")
	cmd.Flags().Int("buffer", 0, "PTY output buffer size in bytes (default 64 KiB)")
	cmd.Flags().String("attach-tmux", "", "attach an existing tmux target instead of spawning an agent")
	cmd.Flags().Bool("debug", false, "enable debug logs")
	cmd.Flags().String("log-file", "", "daemon log file (default: state log dir/onibi.log)")
	return cmd
}

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Pair this machine with a Telegram bot",
		RunE:  runSetup,
	}
	cmd.Flags().Bool("rotate-owner", false, "regenerate owner and require re-pair")
	cmd.Flags().Bool("enable-totp", false, "enable opt-in TOTP gate for destructive commands")
	cmd.Flags().Bool("paranoid", false, "enable TOTP + 60s approval expiry + confirm-tap on presets")
	cmd.Flags().Bool("print-checklist", false, "print setup security checklist and exit")
	cmd.Flags().Bool("token-stdin", false, "read bot token from stdin (avoids argv leak)")
	cmd.Flags().Bool("complete", false, "after pairing, offer service install, hook install, and doctor")
	return cmd
}

func getChatIDCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-chat-id",
		Short: "Print your chat id (fallback for users who can't use deeplinks)",
		RunE:  runGetChatID,
	}
}

func rotateTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-token",
		Short: "Walk through @BotFather /revoke flow and replace the token in Keychain",
		RunE:  runRotateToken,
	}
}

func doctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run setup + integrity checks",
		RunE:  runDoctor,
	}
	cmd.Flags().Bool("offline", false, "skip live Telegram network checks")
	cmd.Flags().String("mode", "auto", "doctor mode (auto, preflight, installed, ci)")
	cmd.Flags().Bool("fix", false, "apply safe local fixes for doctor warnings")
	return cmd
}

func installHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install-hooks",
		Short: "Install agent and/or shell hooks",
		RunE:  runInstallHooks,
	}
	cmd.Flags().String("agent", "", "agent name (claude, codex, opencode, goose, gemini, copilot, pi, amp)")
	cmd.Flags().String("shell", "", "shell name (zsh, bash, fish)")
	cmd.Flags().Bool("all", false, "install every supported agent adapter")
	cmd.Flags().Bool("interactive", false, "prompt for each detected agent/shell")
	cmd.Flags().Bool("uninstall", false, "remove Onibi-managed hooks for the selected agent/shell")
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
		Use:   "adapters",
		Short: "Show agent and shell adapter status",
		RunE:  runAdapters,
	}
	cmd.Flags().Bool("json", false, "print JSON")
	return cmd
}

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List sessions recorded by the daemon",
		RunE:  runSessions,
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
