package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	ircNickEnv       = "ONIBI_IRC_NICK"
	ircAccountEnv    = "ONIBI_IRC_ACCOUNT"
	ircPasswordEnv   = "ONIBI_IRC_PASSWORD"
	ircOwnerNickEnv  = "ONIBI_IRC_OWNER_NICK"
	ircOwnerTokenEnv = "ONIBI_IRC_OWNER_TOKEN"
)

var newIRCClient = irc.NewClient

type ircSettings struct {
	Nick       string
	Account    string
	Password   string
	OwnerNick  string
	OwnerToken string
	Backend    string
}

func (s ircSettings) clientConfig() irc.Config {
	return irc.Config{Nick: s.Nick, Account: s.Account, Password: s.Password}
}

func (s ircSettings) valid() error {
	if err := s.clientConfig().Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.OwnerNick) == "" {
		return errors.New("irc owner nick required")
	}
	if len(strings.TrimSpace(s.OwnerToken)) < 32 {
		return errors.New("irc owner token missing or too short; run `onibi irc setup`")
	}
	return nil
}

func ircCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "irc", Short: "Manage the experimental Libera IRC cockpit", RunE: runIRCStatus}
	setup := &cobra.Command{Use: "setup", Short: "Store Libera SASL and owner-token credentials", RunE: runIRCSetup}
	setup.Flags().String("nick", "", "registered bot nickname")
	setup.Flags().String("account", "", "Libera SASL account")
	setup.Flags().String("password", "", "Libera SASL password")
	setup.Flags().String("owner-nick", "", "IRC nickname that receives cockpit output")
	setup.Flags().String("owner-token", "", "owner token; generated when omitted")
	setup.Flags().Bool("no-check", false, "store credentials without a live TLS/SASL check")
	status := &cobra.Command{Use: "status", Short: "Show IRC setup state", RunE: runIRCStatus}
	status.Flags().Bool("json", false, "print JSON")
	status.Flags().Bool("check", false, "validate TLS and SASL against Libera")
	disable := &cobra.Command{Use: "disable", Short: "Remove IRC credentials", RunE: runIRCDisable}
	cmd.AddCommand(setup, status, disable)
	return cmd
}

func runIRCSetup(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStoreForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	settings := ircSettings{}
	for _, field := range []struct {
		name string
		into *string
	}{
		{"nick", &settings.Nick},
		{"account", &settings.Account},
		{"password", &settings.Password},
		{"owner-nick", &settings.OwnerNick},
	} {
		value, _ := cmd.Flags().GetString(field.name)
		if strings.TrimSpace(value) == "" {
			value, err = promptIRCField(cmd, field.name)
			if err != nil {
				return err
			}
		}
		*field.into = strings.TrimSpace(value)
	}
	settings.OwnerToken, _ = cmd.Flags().GetString("owner-token")
	settings.OwnerToken = strings.TrimSpace(settings.OwnerToken)
	generated := settings.OwnerToken == ""
	if generated {
		settings.OwnerToken, err = newIRCOwnerToken()
		if err != nil {
			return err
		}
	}
	if err := settings.valid(); err != nil {
		return err
	}
	noCheck, _ := cmd.Flags().GetBool("no-check")
	if !noCheck {
		if err := checkIRC(cmd.Context(), settings); err != nil {
			return err
		}
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		daemon.IRCSecretNick: settings.Nick, daemon.IRCSecretAccount: settings.Account, daemon.IRCSecretPassword: settings.Password,
		daemon.IRCSecretOwnerNick: settings.OwnerNick, daemon.IRCSecretOwnerToken: settings.OwnerToken,
	} {
		if err := st.Set(key, value); err != nil {
			return err
		}
	}
	if noCheck {
		fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Stored IRC credentials (live check skipped).")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Stored and validated Libera IRC credentials.")
	}
	if generated {
		fmt.Fprintln(cmd.OutOrStdout(), "Owner token (shown once):", settings.OwnerToken)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "DM syntax: !onibi <token> <text>; start with `onibi start --transport=irc`.\n")
	return nil
}

func runIRCStatus(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStoreForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	settings, err := loadIRCSettings(cmd.Context(), paths)
	configured := err == nil && settings.valid() == nil
	check, _ := cmd.Flags().GetBool("check")
	report := ircStatusReport{Configured: configured, SecretBackend: settings.Backend, Check: check, Endpoint: irc.DefaultHost + ":" + irc.DefaultPort}
	if err != nil {
		report.Error = err.Error()
	}
	if check && configured {
		if err := checkIRC(cmd.Context(), settings); err != nil {
			report.Reachable = boolPtr(false)
			report.Error = err.Error()
		} else {
			report.Reachable = boolPtr(true)
		}
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	rows := [][]string{{"configured", styleFor(cmd).bool(report.Configured), report.SecretBackend}, {"endpoint", "yes", report.Endpoint}}
	if check {
		rows = append(rows, []string{"tls_sasl", styleFor(cmd).bool(report.Reachable != nil && *report.Reachable), report.Error})
	}
	return renderTable(cmd.OutOrStdout(), rows)
}

type ircStatusReport struct {
	Configured    bool   `json:"configured"`
	SecretBackend string `json:"secret_backend,omitempty"`
	Check         bool   `json:"check"`
	Reachable     *bool  `json:"reachable,omitempty"`
	Endpoint      string `json:"endpoint"`
	Error         string `json:"error,omitempty"`
}

func runIRCDisable(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStoreForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	var errs []error
	for _, key := range []string{daemon.IRCSecretNick, daemon.IRCSecretAccount, daemon.IRCSecretPassword, daemon.IRCSecretOwnerNick, daemon.IRCSecretOwnerToken} {
		errs = append(errs, st.Delete(key))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "IRC disabled.")
	return nil
}

func runIRCUp(cmd *cobra.Command, paths config.Paths, db *store.DB, cfg config.Config, logger *slog.Logger, started time.Time, shellCWD string) error {
	settings, err := loadIRCSettings(cmd.Context(), paths)
	if err != nil {
		return err
	}
	if err := settings.valid(); err != nil {
		return err
	}
	client := newIRCClient(settings.clientConfig())
	checkCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	err = client.Connect(checkCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("irc connect: %w", err)
	}
	d := daemon.New(daemon.Options{
		Paths: paths, DB: db, Log: logger,
		ApprovalTTL: cfg.Daemon.ApprovalTimeout.Std(), ApprovalSweepInterval: cfg.Daemon.ApprovalSweepInterval.Std(), ApprovalMaxSubscribers: cfg.Daemon.MaxSubscribers,
		IdleThreshold: cfg.Daemon.TurnIdleThreshold.Std(), IdleInterval: cfg.Daemon.TurnIdleInterval.Std(), BufferSize: cfg.Daemon.PTYBufferBytes,
		TerminalDefault: cfg.Terminal.Default, IRCClient: client, IRCOwnerNick: settings.OwnerNick, IRCOwnerToken: settings.OwnerToken,
		ProviderOutput: daemonProviderOutputPolicy(cfg), ProviderOutputOverrides: daemonProviderOutputOverrides(cfg), SkipRestore: true,
	})
	session, err := startManagedWebPairShell(cmd.Context(), d, cfg, shellCWD, logger)
	if err != nil {
		_ = client.Close()
		return err
	}
	defer cleanupManagedWebPairShell(logger, d, session.ID, session.TmuxTarget)
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), settings.OwnerNick)
	} else {
		printCLIHeader(cmd, "IRC experimental")
		fmt.Fprintln(cmd.OutOrStdout(), "Network:", irc.DefaultHost+":"+irc.DefaultPort)
		fmt.Fprintln(cmd.OutOrStdout(), "Bot:", settings.Nick)
		fmt.Fprintln(cmd.OutOrStdout(), "Owner:", settings.OwnerNick)
		fmt.Fprintln(cmd.OutOrStdout(), "Session:", session.ID)
		fmt.Fprintln(cmd.OutOrStdout(), "DM: !onibi <token> <text>. IRC is not end-to-end encrypted. Press Ctrl-C to stop.")
	}
	logger.Info("onibi irc ready", "uptime_ms", time.Since(started).Milliseconds(), "nick", settings.Nick, "owner", settings.OwnerNick)
	return d.Run(cmd.Context())
}

func loadIRCSettings(ctx context.Context, paths config.Paths) (ircSettings, error) {
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return ircSettings{}, err
	}
	values := map[string]*string{}
	settings := ircSettings{Backend: string(st.Backend())}
	values[daemon.IRCSecretNick] = &settings.Nick
	values[daemon.IRCSecretAccount] = &settings.Account
	values[daemon.IRCSecretPassword] = &settings.Password
	values[daemon.IRCSecretOwnerNick] = &settings.OwnerNick
	values[daemon.IRCSecretOwnerToken] = &settings.OwnerToken
	for key, dst := range values {
		value, ok, err := st.GetWithTimeout(ctx, key, time.Second)
		if err != nil {
			return ircSettings{}, err
		}
		if ok {
			*dst = strings.TrimSpace(value)
		}
	}
	for env, dst := range map[string]*string{ircNickEnv: &settings.Nick, ircAccountEnv: &settings.Account, ircPasswordEnv: &settings.Password, ircOwnerNickEnv: &settings.OwnerNick, ircOwnerTokenEnv: &settings.OwnerToken} {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			*dst = value
			settings.Backend = "env+" + settings.Backend
		}
	}
	return settings, nil
}

func checkIRC(ctx context.Context, settings ircSettings) error {
	client := newIRCClient(settings.clientConfig())
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	defer client.Close()
	return client.Connect(ctx)
}

func promptIRCField(cmd *cobra.Command, name string) (string, error) {
	if !inputIsTerminal(cmd.InOrStdin()) {
		return "", fmt.Errorf("--%s required when stdin is not a terminal", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: ", name)
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		return "", sc.Err()
	}
	value := strings.TrimSpace(sc.Text())
	if value == "" {
		return "", fmt.Errorf("%s required", name)
	}
	return value, nil
}

func newIRCOwnerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
