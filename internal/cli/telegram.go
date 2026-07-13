package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

var newTelegramClient = telegram.NewClient
var openSecretStore = secrets.Open

const telegramTokenEnv = "ONIBI_TELEGRAM_TOKEN"

func telegramCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telegram",
		Short: "Manage Telegram chat transport",
		RunE:  runTelegramStatus,
	}
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Store and validate a Telegram bot token",
		RunE:  runTelegramSetup,
	}
	setup.Flags().String("token", "", "BotFather token")
	setup.Flags().Bool("no-check", false, "store token after local syntax validation only")
	status := &cobra.Command{
		Use:   "status",
		Short: "Show Telegram setup state",
		RunE:  runTelegramStatus,
	}
	status.Flags().Bool("json", false, "print JSON")
	status.Flags().Bool("check", false, "validate token with Telegram getMe")
	disable := &cobra.Command{
		Use:   "disable",
		Short: "Remove Telegram token and owner pairing",
		RunE:  runTelegramDisable,
	}
	cmd.AddCommand(setup, status, disable)
	return cmd
}

func runTelegramSetup(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) == "" {
		token, err = promptTelegramToken(cmd)
		if err != nil {
			return err
		}
	}
	noCheck, _ := cmd.Flags().GetBool("no-check")
	var user telegram.User
	if noCheck {
		if !telegram.ValidBotToken(token) {
			return errors.New("token does not look like a BotFather token")
		}
	} else {
		user, err = validateTelegramToken(cmd.Context(), token)
		if err != nil {
			return err
		}
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if err := st.Set(daemon.TelegramSecretBotToken, strings.TrimSpace(token)); err != nil {
		return err
	}
	if noCheck {
		fmt.Fprintf(cmd.OutOrStdout(), "%s Stored Telegram bot token (live check skipped)\n", styleFor(cmd).green("[OK]"))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s Stored Telegram bot @%s\n", styleFor(cmd).green("[OK]"), user.Username)
	}
	if _, ok, _ := db.KVGetString(cmd.Context(), daemon.TelegramKVOwnerChatID); !ok {
		code, err := ensureTelegramPairCode(cmd.Context(), db)
		if err != nil {
			return err
		}
		if noCheck {
			fmt.Fprintf(cmd.OutOrStdout(), "Pair: send /start %s while `onibi up --transport=telegram` is running.\n", code)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Pair: send /start %s to @%s while `onibi up --transport=telegram` is running.\n", code, user.Username)
		}
	}
	if noCheck {
		fmt.Fprintln(cmd.OutOrStdout(), "Check: onibi telegram status --check")
	}
	return nil
}

func runTelegramStatus(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	token, tokenOK, _ := st.Get(daemon.TelegramSecretBotToken)
	secretBackend := string(st.Backend())
	if envToken := strings.TrimSpace(os.Getenv(telegramTokenEnv)); envToken != "" {
		token = envToken
		tokenOK = true
		secretBackend = "env"
	}
	owner, ownerOK, _ := db.KVGetString(cmd.Context(), daemon.TelegramKVOwnerChatID)
	liveCheck, _ := cmd.Flags().GetBool("check")
	report := telegramStatusReport{
		Token:         tokenOK,
		SecretBackend: secretBackend,
		OwnerPaired:   ownerOK,
		OwnerChatID:   owner,
		Check:         liveCheck,
	}
	if liveCheck && tokenOK {
		user, err := validateTelegramToken(cmd.Context(), token)
		if err != nil {
			report.TokenValid = boolPtr(false)
			report.CheckError = err.Error()
		} else {
			report.TokenValid = boolPtr(true)
			report.BotID = user.ID
			report.BotUsername = user.Username
		}
	}
	report.Next = telegramStatusNext(report)
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	style := styleFor(cmd)
	rows := [][]string{
		{"token", style.bool(report.Token), report.SecretBackend},
		{"owner_chat_id", style.bool(ownerOK), owner},
	}
	if liveCheck {
		valid := report.TokenValid != nil && *report.TokenValid
		detail := report.CheckError
		if detail == "" && report.BotUsername != "" {
			detail = "@" + report.BotUsername
		}
		rows = append(rows, []string{"token_valid", style.bool(valid), detail})
	}
	return renderTable(cmd.OutOrStdout(), rows)
}

type telegramStatusReport struct {
	Token         bool     `json:"token"`
	SecretBackend string   `json:"secret_backend"`
	OwnerPaired   bool     `json:"owner_paired"`
	OwnerChatID   string   `json:"owner_chat_id,omitempty"`
	Check         bool     `json:"check"`
	TokenValid    *bool    `json:"token_valid,omitempty"`
	BotID         int64    `json:"bot_id,omitempty"`
	BotUsername   string   `json:"bot_username,omitempty"`
	CheckError    string   `json:"check_error,omitempty"`
	Next          []string `json:"next,omitempty"`
}

func telegramStatusNext(report telegramStatusReport) []string {
	var next []string
	if !report.Token {
		next = append(next, "onibi telegram setup")
		return next
	}
	if report.Check && report.TokenValid != nil && !*report.TokenValid {
		if report.SecretBackend == "env" {
			next = append(next, "set "+telegramTokenEnv)
		} else {
			next = append(next, "onibi telegram setup")
		}
		return next
	}
	if !report.OwnerPaired {
		next = append(next, "onibi up --transport=telegram")
	}
	return next
}

func boolPtr(v bool) *bool { return &v }

func runTelegramDisable(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	_ = st.Delete(daemon.TelegramSecretBotToken)
	_ = db.KVDel(cmd.Context(), daemon.TelegramKVOwnerChatID)
	_ = db.KVDel(cmd.Context(), daemon.TelegramKVPairCode)
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Telegram disabled.")
	return nil
}

func runTelegramUp(cmd *cobra.Command, paths config.Paths, db *store.DB, cfg config.Config, logger *slog.Logger, started time.Time, shellCWD string) error {
	if !cfg.Experimental.Providers {
		return errors.New("telegram is deferred in v1; set experimental.providers=true to opt into unsupported provider behavior")
	}
	token, botUser, err := telegramTokenForUp(cmd, paths)
	if err != nil {
		return err
	}
	ownerID, err := telegramOwnerID(cmd.Context(), db)
	if err != nil {
		return err
	}
	pairCode := ""
	if ownerID == 0 {
		pairCode, err = ensureTelegramPairCode(cmd.Context(), db)
		if err != nil {
			return err
		}
	}
	d := daemon.New(daemon.Options{
		Paths:                  paths,
		DB:                     db,
		Log:                    logger,
		ApprovalTTL:            cfg.Daemon.ApprovalTimeout.Std(),
		ApprovalSweepInterval:  cfg.Daemon.ApprovalSweepInterval.Std(),
		ApprovalMaxSubscribers: cfg.Daemon.MaxSubscribers,
		IdleThreshold:          cfg.Daemon.TurnIdleThreshold.Std(),
		IdleInterval:           cfg.Daemon.TurnIdleInterval.Std(),
		BufferSize:             cfg.Daemon.PTYBufferBytes,
		TerminalDefault:        cfg.Terminal.Default,
		ExperimentalProviders:  cfg.Experimental.Providers,
		TelegramToken:          token,
		TelegramOwnerID:        ownerID,
		TelegramPair:           pairCode,
		UpdateAuto:             cfg.Update.Auto,
		UpdateChannel:          cfg.Update.Channel,
		SkipRestore:            true,
	})
	session, err := startManagedWebPairShell(cmd.Context(), d, cfg, shellCWD, logger)
	if err != nil {
		return err
	}
	defer cleanupManagedWebPairShell(logger, d, session.ID, session.TmuxTarget)
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), "@"+botUser.Username)
	} else {
		printCLIHeader(cmd, "Telegram")
		fmt.Fprintln(cmd.OutOrStdout(), "Bot:", "@"+botUser.Username)
		if ownerID == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Pair:", "/start "+pairCode)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Owner:", ownerID)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Session:", session.ID)
		fmt.Fprintln(cmd.OutOrStdout(), "Text the bot to send input. Press Ctrl-C to stop.")
	}
	logger.Info("onibi telegram ready", "uptime_ms", time.Since(started).Milliseconds(), "bot", botUser.Username, "owner", ownerID != 0)
	return d.Run(cmd.Context())
}

func telegramTokenForUp(cmd *cobra.Command, paths config.Paths) (string, telegram.User, error) {
	if token := strings.TrimSpace(os.Getenv(telegramTokenEnv)); token != "" {
		u, err := validateTelegramToken(cmd.Context(), token)
		return token, u, err
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return "", telegram.User{}, err
	}
	if token, ok, err := st.Get(daemon.TelegramSecretBotToken); err != nil {
		return "", telegram.User{}, err
	} else if ok && strings.TrimSpace(token) != "" {
		u, err := validateTelegramToken(cmd.Context(), token)
		return token, u, err
	}
	if !inputIsTerminal(cmd.InOrStdin()) {
		return "", telegram.User{}, errors.New("telegram token missing; run `onibi telegram setup` or set " + telegramTokenEnv)
	}
	token, err := promptTelegramToken(cmd)
	if err != nil {
		return "", telegram.User{}, err
	}
	u, err := validateTelegramToken(cmd.Context(), token)
	if err != nil {
		return "", telegram.User{}, err
	}
	if err := st.Set(daemon.TelegramSecretBotToken, strings.TrimSpace(token)); err != nil {
		return "", telegram.User{}, err
	}
	return token, u, nil
}

func promptTelegramToken(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Paste Telegram BotFather token: ")
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		return "", sc.Err()
	}
	token := strings.TrimSpace(sc.Text())
	if !telegram.ValidBotToken(token) {
		return "", errors.New("token does not look like a BotFather token")
	}
	return token, nil
}

func validateTelegramToken(ctx context.Context, token string) (telegram.User, error) {
	if !telegram.ValidBotToken(token) {
		return telegram.User{}, errors.New("token does not look like a BotFather token")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return newTelegramClient(token).GetMe(ctx)
}

func telegramOwnerID(ctx context.Context, db *store.DB) (int64, error) {
	v, ok, err := db.KVGetString(ctx, daemon.TelegramKVOwnerChatID)
	if err != nil || !ok {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func ensureTelegramPairCode(ctx context.Context, db *store.DB) (string, error) {
	if code, ok, err := db.KVGetString(ctx, daemon.TelegramKVPairCode); err != nil {
		return "", err
	} else if ok && strings.TrimSpace(code) != "" {
		return code, nil
	}
	code, err := daemon.NewTelegramPairCode()
	if err != nil {
		return "", err
	}
	return code, db.KVSet(ctx, daemon.TelegramKVPairCode, []byte(code), time.Now().Add(10*time.Minute).Unix())
}
