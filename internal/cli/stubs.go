package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// errNotImplemented is returned by unfinished subcommands.
var errNotImplemented = errors.New("not implemented yet")

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [agent [args...]]",
		Short: "Start the daemon (with an optional agent spawn)",
		RunE:  runRun,
	}
	cmd.Flags().String("name", "", "session label (defaults to agent name)")
	cmd.Flags().Int("buffer", 0, "PTY output buffer size in bytes (default 64 KiB)")
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

func sessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List active sessions hosted by the daemon (phase 6)",
		RunE:  func(*cobra.Command, []string) error { return errNotImplemented },
	}
}

func logCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Print recent audit-log entries (phase 10)",
		RunE:  func(*cobra.Command, []string) error { return errNotImplemented },
	}
	cmd.Flags().Int("n", 50, "number of entries to print")
	cmd.Flags().String("export", "", "export full log to file (csv|json by extension)")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build info",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("onibi v2-dev")
		},
	}
}
