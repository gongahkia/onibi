package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
)

// runSetup implements `onibi setup`.
func runSetup(cmd *cobra.Command, _ []string) error {
	rotateOwner, _ := cmd.Flags().GetBool("rotate-owner")
	enableTOTP, _ := cmd.Flags().GetBool("enable-totp")
	paranoid, _ := cmd.Flags().GetBool("paranoid")
	printChecklist, _ := cmd.Flags().GetBool("print-checklist")
	tokenStdin, _ := cmd.Flags().GetBool("token-stdin")
	complete, _ := cmd.Flags().GetBool("complete")

	if printChecklist {
		printSetupChecklist(cmd.OutOrStdout())
		return nil
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}

	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()

	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if sec.Backend() == secrets.BackendDotenv {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"warning: OS keystore unavailable; falling back to %s (0600). Consider installing libsecret/keyring.\n",
			paths.EnvFile)
	}

	flags := setup.Flags{
		RotateOwner: rotateOwner,
		EnableTOTP:  enableTOTP,
		Paranoid:    paranoid,
		TokenStdin:  tokenStdin,
		PairTimeout: 10 * time.Minute,
	}
	io := setup.IO{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	if _, err := setup.Run(ctx, db, sec, flags, io); err != nil {
		if complete && errors.Is(err, setup.ErrOwnerAlreadyPaired) {
			return runSetupComplete(cmd, paths, db)
		}
		return err
	}
	if complete {
		return runSetupComplete(cmd, paths, db)
	}
	return nil
}

func runSetupComplete(cmd *cobra.Command, paths config.Paths, db *store.DB) error {
	br := bufio.NewReader(cmd.InOrStdin())
	if askYesNo(cmd, br, "Install and start background service? [Y/n] ", true) {
		m, err := service.NewManager(paths, "")
		if err != nil {
			return err
		}
		if err := m.Install(cmd.Context()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Service installed.")
	}
	if askYesNo(cmd, br, "Auto-detect and install agent/shell hooks? [Y/n] ", true) {
		notifyBin, err := locateNotifyBinary()
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+err.Error())
		} else {
			cfg, _, err := config.Load(paths)
			if err != nil {
				return err
			}
			if err := runInteractiveHooks(cmd, db, notifyBin, false, cfg.Shell.MinDuration.Std().Milliseconds()); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nDoctor summary:")
	report := doctor.Run(cmd.Context(), doctor.Options{Paths: paths, Mode: "installed"})
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
	}
	if report.Failed() {
		return fmt.Errorf("setup complete but doctor failed")
	}
	return nil
}

func askYesNo(cmd *cobra.Command, br *bufio.Reader, prompt string, def bool) bool {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	line, _ := br.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes"
}

// runGetChatID is the fallback for users who can't use deeplinks: it opens
// the long-poll, prints any chat id that messages the bot, and exits.
func runGetChatID(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	tok, ok, err := sec.Get(secrets.KeyBotToken)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no bot token stored — run `onibi setup` first")
	}
	io := setup.IO{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
	fmt.Fprintln(io.Out, "Sending any message to your bot will print the chat id and exit.")
	return setup.RunGetChatID(cmd.Context(), tok, io)
}

// runRotateToken prompts for a fresh BotFather token, validates it via
// getMe, ensures the bot id matches the persisted bot_id (refusing if it
// doesn't — would indicate a different bot), and replaces the stored token.
func runRotateToken(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	io := setup.IO{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
	fmt.Fprintln(io.Out, "Walk through @BotFather → /mybots → select bot → API Token → Revoke current token.")
	fmt.Fprintln(io.Out, "BotFather replies with a new token. Paste it below.")
	return setup.RunRotateToken(cmd.Context(), db, sec, io)
}

func printSetupChecklist(out interface{ Write([]byte) (int, error) }) {
	body := `Setup security checklist:

  [ ] Bot username is unguessable (>=4 random hex chars)
  [ ] Bot token stored in OS keystore (not .env)
  [ ] Telegram 2-step verification enabled on owner account
  [ ] Telegram 2FA recovery is EMAIL, not SMS
  [ ] LaunchAgent / systemd user unit loaded
  [ ] State dir 0700, socket 0600 (run: onibi doctor)
  [ ] Last token rotation within 6 months
  [ ] All installed hook hashes match registry
`
	_, _ = out.Write([]byte(body))
}
