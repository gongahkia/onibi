package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

var doctorRun = doctor.Run
var inputIsTerminal = func(in any) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// runSetup implements `onibi setup`.
func runSetup(cmd *cobra.Command, _ []string) error {
	rotateOwner, _ := cmd.Flags().GetBool("rotate-owner")
	enableTOTP, _ := cmd.Flags().GetBool("enable-totp")
	paranoid, _ := cmd.Flags().GetBool("paranoid")
	printChecklist, _ := cmd.Flags().GetBool("print-checklist")
	tokenStdin, _ := cmd.Flags().GetBool("token-stdin")
	complete, _ := cmd.Flags().GetBool("complete")
	enableEncrypted, _ := cmd.Flags().GetBool("enable-encrypted-mode")
	encryptedMode, _ := cmd.Flags().GetString("encrypted-mode")
	miniAppURL, _ := cmd.Flags().GetString("mini-app-url")

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
		if enableEncrypted && errors.Is(err, setup.ErrOwnerAlreadyPaired) {
			if err := runSetupEncrypted(cmd, paths, sec, encryptedMode, miniAppURL); err != nil {
				return err
			}
			if complete {
				return runSetupComplete(cmd, paths, db)
			}
			return nil
		}
		if complete && errors.Is(err, setup.ErrOwnerAlreadyPaired) {
			return runSetupComplete(cmd, paths, db)
		}
		if errors.Is(err, setup.ErrOwnerAlreadyPaired) {
			return ownerAlreadyPairedHelp(paths)
		}
		return err
	}
	if enableEncrypted {
		if err := runSetupEncrypted(cmd, paths, sec, encryptedMode, miniAppURL); err != nil {
			return err
		}
	}
	if complete {
		return runSetupComplete(cmd, paths, db)
	}
	printSetupNextActions(cmd)
	sendSetupOnboarding(ctx, cmd, paths, db)
	return nil
}

func ownerAlreadyPairedHelp(paths config.Paths) error {
	return fmt.Errorf(`already paired with Onibi.

State:
  db     %s
  owner  already recorded

Next:
  onibi setup --complete       finish service/hooks/doctor setup
  onibi rotate-token           keep owner; replace bot token
  onibi setup --rotate-owner   replace owner and pair again`, paths.DBFile)
}

func runSetupEncrypted(cmd *cobra.Command, paths config.Paths, sec *secrets.Store, mode, miniAppURL string) error {
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	if strings.TrimSpace(mode) == "" {
		mode = "on"
	}
	if err := config.Set(&cfg, "telegram.encrypted_mode", mode); err != nil {
		return err
	}
	if strings.TrimSpace(miniAppURL) != "" {
		if err := config.Set(&cfg, "telegram.mini_app_url", miniAppURL); err != nil {
			return err
		}
	}
	seed, ok, err := sec.Get(secrets.KeyEnvelopeSeed)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(seed) == "" {
		seed, err = envelope.GenerateSeed()
		if err != nil {
			return err
		}
		if err := sec.Set(secrets.KeyEnvelopeSeed, seed); err != nil {
			return err
		}
	}
	if err := config.Save(paths.Config, cfg); err != nil {
		return err
	}
	seedURL, err := envelope.BuildSeedURL(cfg.Telegram.MiniAppURL, seed)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintf(cmd.OutOrStdout(), "Legacy encrypted mode set to %s.\n", cfg.Telegram.EncryptedMode)
	fmt.Fprintln(cmd.OutOrStdout(), "Open this legacy seed URL to store the Mini App decrypt seed:")
	fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", seedURL)
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Or scan this legacy QR:")
	if err := setup.PrintQR(cmd.OutOrStdout(), seedURL); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "(QR failed: %v)\n", err)
		fmt.Fprintln(cmd.OutOrStdout(), "Use the URL above.")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Security note: this is the legacy encrypted path. Keep the Mini App host static and audited. Plaintext entry will refuse in encrypted mode; use /secure or switch to encrypted_mode=off.")
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
			if err := handleMissingNotifyBinary(cmd, br, err); err != nil {
				return err
			}
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
	style := styleFor(cmd)
	report := doctorRun(cmd.Context(), doctor.Options{Paths: paths, Mode: "installed"})
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", style.status(c.Status), c.Name, c.Detail)
	}
	if report.Failed() {
		return fmt.Errorf("setup complete but doctor failed")
	}
	printSetupNextActions(cmd)
	sendSetupOnboarding(cmd.Context(), cmd, paths, db)
	return nil
}

func printSetupNextActions(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), "\nNext:")
	fmt.Fprintln(cmd.OutOrStdout(), "  onibi demo --approval")
	fmt.Fprintln(cmd.OutOrStdout(), "  /menu")
	fmt.Fprintln(cmd.OutOrStdout(), "  /project add <alias> <path>")
	fmt.Fprintln(cmd.OutOrStdout(), "  /new --visible --project <alias> shell")
}

func sendSetupOnboarding(ctx context.Context, cmd *cobra.Command, paths config.Paths, db *store.DB) {
	if ctx == nil {
		ctx = context.Background()
	}
	owner, err := auth.LoadOwner(ctx, db)
	if err != nil {
		return
	}
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return
	}
	token, ok, err := sec.GetWithTimeout(ctx, secrets.KeyBotToken, 5*time.Second)
	if err != nil || !ok || strings.TrimSpace(token) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bot, err := telegram.New(ctx, telegram.Options{Token: token, OffsetStore: db})
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: onboarding Telegram send skipped: %v\n", err)
		return
	}
	_, err = bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      owner.ID(),
		Text:        onboardingText(),
		ReplyMarkup: telegram.OnboardingKeyboard(),
	})
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: onboarding Telegram send failed: %v\n", err)
	}
}

func onboardingText() string {
	return "Onibi setup complete.\n\nChoose next action:"
}

func handleMissingNotifyBinary(cmd *cobra.Command, br *bufio.Reader, cause error) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "")
	fmt.Fprintln(cmd.ErrOrStderr(), "onibi-notify not found. Remediation:")
	fmt.Fprintln(cmd.ErrOrStderr(), "  1) make install")
	fmt.Fprintln(cmd.ErrOrStderr(), "  2) export ONIBI_NOTIFY_BIN=/abs/path/to/onibi-notify")
	fmt.Fprintln(cmd.ErrOrStderr(), "  3) onibi adapters")
	fmt.Fprintln(cmd.ErrOrStderr(), "  4) onibi install-hooks --interactive")
	if inputIsTerminal(cmd.InOrStdin()) && askYesNo(cmd, br, "Continue without hooks? [y/N] ", false) {
		return nil
	}
	return fmt.Errorf("hooks step aborted: onibi-notify missing: %w", cause)
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

// runRotateToken prompts for a fresh legacy token, validates it via
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
	fmt.Fprintln(io.Out, "Legacy token rotation: revoke the old token with the provider and paste the replacement below.")
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
