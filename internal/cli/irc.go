package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

func ircCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "irc", Short: "Manage IRC chat transport", RunE: runIRCStatus, PersistentPreRunE: requireExperimentalProviders}
	setup := &cobra.Command{Use: "setup", Short: "Store IRC TLS/SASL settings", RunE: runIRCSetup}
	setup.Flags().String("address", irc.DefaultAddress, "TLS server address")
	setup.Flags().String("nick", "", "registered bot nick")
	setup.Flags().String("username", "", "SASL username")
	setup.Flags().String("password", "", "SASL password")
	setup.Flags().String("owner-nick", "", "owner DM nick")
	setup.Flags().String("owner-account", "", "owner services account")
	status := &cobra.Command{Use: "status", Short: "Show IRC setup state", RunE: runIRCStatus}
	status.Flags().Bool("json", false, "print JSON")
	disable := &cobra.Command{Use: "disable", Short: "Remove IRC TLS/SASL settings", RunE: runIRCDisable}
	cmd.AddCommand(setup, status, disable)
	return cmd
}

func runIRCSetup(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStoreForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	address, _ := cmd.Flags().GetString("address")
	nick, _ := cmd.Flags().GetString("nick")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	ownerNick, _ := cmd.Flags().GetString("owner-nick")
	ownerAccount, _ := cmd.Flags().GetString("owner-account")
	if err := irc.ValidateConfig(irc.Config{Address: address, Nick: nick, Username: username, Password: password}); err != nil {
		return err
	}
	if err := validateIRCIdentity("owner nick", ownerNick); err != nil {
		return err
	}
	if err := validateIRCIdentity("owner account", ownerAccount); err != nil {
		return err
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if err := st.Set(daemon.IRCSecretSASLPassword, strings.TrimSpace(password)); err != nil {
		return err
	}
	for key, value := range map[string]string{
		daemon.IRCKVAddress:      strings.TrimSpace(address),
		daemon.IRCKVNick:         strings.TrimSpace(nick),
		daemon.IRCKVUsername:     strings.TrimSpace(username),
		daemon.IRCKVOwnerNick:    strings.TrimSpace(ownerNick),
		daemon.IRCKVOwnerAccount: strings.TrimSpace(ownerAccount),
	} {
		if err := db.KVSetString(cmd.Context(), key, value); err != nil {
			return err
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "Stored IRC TLS/SASL settings.")
	fmt.Fprintln(cmd.OutOrStdout(), "Run: onibi up --transport=irc")
	return nil
}

type ircStatusReport struct {
	Configured   bool     `json:"configured"`
	Password     bool     `json:"password"`
	Address      string   `json:"address,omitempty"`
	Nick         string   `json:"nick,omitempty"`
	Username     string   `json:"username,omitempty"`
	OwnerNick    string   `json:"owner_nick,omitempty"`
	OwnerAccount string   `json:"owner_account,omitempty"`
	Next         []string `json:"next,omitempty"`
}

func runIRCStatus(cmd *cobra.Command, _ []string) error {
	paths, db, err := openCLIStoreForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	values, complete, err := ircState(cmd.Context(), db)
	if err != nil {
		return err
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	_, passwordOK, err := st.Get(daemon.IRCSecretSASLPassword)
	if err != nil {
		return err
	}
	report := ircStatusReport{
		Configured:   complete && passwordOK,
		Password:     passwordOK,
		Address:      values[daemon.IRCKVAddress],
		Nick:         values[daemon.IRCKVNick],
		Username:     values[daemon.IRCKVUsername],
		OwnerNick:    values[daemon.IRCKVOwnerNick],
		OwnerAccount: values[daemon.IRCKVOwnerAccount],
	}
	if !report.Configured {
		report.Next = []string{"onibi experimental irc setup"}
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	}
	return renderTable(cmd.OutOrStdout(), [][]string{
		{"configured", styleFor(cmd).bool(report.Configured), ""},
		{"password", styleFor(cmd).bool(report.Password), "secret storage"},
		{"address", styleFor(cmd).bool(report.Address != ""), report.Address},
		{"nick", styleFor(cmd).bool(report.Nick != ""), report.Nick},
		{"username", styleFor(cmd).bool(report.Username != ""), report.Username},
		{"owner_nick", styleFor(cmd).bool(report.OwnerNick != ""), report.OwnerNick},
		{"owner_account", styleFor(cmd).bool(report.OwnerAccount != ""), report.OwnerAccount},
	})
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
	_ = st.Delete(daemon.IRCSecretSASLPassword)
	for _, key := range []string{daemon.IRCKVAddress, daemon.IRCKVNick, daemon.IRCKVUsername, daemon.IRCKVOwnerNick, daemon.IRCKVOwnerAccount} {
		_ = db.KVDel(cmd.Context(), key)
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).green("[OK]"), "IRC disabled.")
	return nil
}

func runIRCUp(cmd *cobra.Command, paths config.Paths, db *store.DB, cfg config.Config, logger *slog.Logger, started time.Time, shellCWD string) error {
	if !cfg.Experimental.Providers {
		return errors.New("irc is deferred in v1; set experimental.providers=true to opt into unsupported provider behavior")
	}
	ircConfig, ownerNick, ownerAccount, err := ircConfigForUp(cmd.Context(), paths, db)
	if err != nil {
		return err
	}
	d := daemon.New(daemon.Options{
		Paths:                   paths,
		DB:                      db,
		Log:                     logger,
		ApprovalTTL:             cfg.Daemon.ApprovalTimeout.Std(),
		ApprovalSweepInterval:   cfg.Daemon.ApprovalSweepInterval.Std(),
		ApprovalMaxSubscribers:  cfg.Daemon.MaxSubscribers,
		IdleThreshold:           cfg.Daemon.TurnIdleThreshold.Std(),
		IdleInterval:            cfg.Daemon.TurnIdleInterval.Std(),
		BufferSize:              cfg.Daemon.PTYBufferBytes,
		TerminalDefault:         cfg.Terminal.Default,
		ExperimentalProviders:   cfg.Experimental.Providers,
		IRCClient:               irc.NewClient(ircConfig),
		IRCNick:                 ircConfig.Nick,
		IRCOwnerNick:            ownerNick,
		IRCOwnerAccount:         ownerAccount,
		ProviderOutput:          daemonProviderOutputPolicy(cfg),
		ProviderOutputOverrides: daemonProviderOutputOverrides(cfg),
		SkipRestore:             true,
	})
	session, err := startManagedWebPairShell(cmd.Context(), d, cfg, shellCWD, logger)
	if err != nil {
		return err
	}
	defer cleanupManagedWebPairShell(logger, d, session.ID, session.TmuxTarget)
	if !quiet(cmd) {
		printCLIHeader(cmd, "IRC")
		fmt.Fprintln(cmd.OutOrStdout(), "Server:", ircConfig.Address)
		fmt.Fprintln(cmd.OutOrStdout(), "Bot:", ircConfig.Nick)
		fmt.Fprintln(cmd.OutOrStdout(), "Owner account:", ownerAccount)
		fmt.Fprintln(cmd.OutOrStdout(), "Session:", session.ID)
		fmt.Fprintln(cmd.OutOrStdout(), "DM the bot from the owner account. Press Ctrl-C to stop.")
	}
	logger.Info("onibi irc ready", "uptime_ms", time.Since(started).Milliseconds(), "server", ircConfig.Address, "nick", ircConfig.Nick)
	return d.Run(cmd.Context())
}

func ircConfigForUp(ctx context.Context, paths config.Paths, db *store.DB) (irc.Config, string, string, error) {
	values, complete, err := ircState(ctx, db)
	if err != nil {
		return irc.Config{}, "", "", err
	}
	if !complete {
		return irc.Config{}, "", "", errors.New("irc settings incomplete; run onibi experimental irc setup")
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return irc.Config{}, "", "", err
	}
	password, ok, err := st.Get(daemon.IRCSecretSASLPassword)
	if err != nil {
		return irc.Config{}, "", "", err
	}
	if !ok {
		return irc.Config{}, "", "", errors.New("irc SASL password missing; run onibi experimental irc setup")
	}
	cfg := irc.Config{Address: values[daemon.IRCKVAddress], Nick: values[daemon.IRCKVNick], Username: values[daemon.IRCKVUsername], Password: password}
	if err := irc.ValidateConfig(cfg); err != nil {
		return irc.Config{}, "", "", err
	}
	return cfg, values[daemon.IRCKVOwnerNick], values[daemon.IRCKVOwnerAccount], nil
}

func ircState(ctx context.Context, db *store.DB) (map[string]string, bool, error) {
	values := map[string]string{}
	complete := true
	for _, key := range []string{daemon.IRCKVAddress, daemon.IRCKVNick, daemon.IRCKVUsername, daemon.IRCKVOwnerNick, daemon.IRCKVOwnerAccount} {
		value, ok, err := db.KVGetString(ctx, key)
		if err != nil {
			return nil, false, err
		}
		values[key] = strings.TrimSpace(value)
		complete = complete && ok && values[key] != ""
	}
	return values, complete, nil
}

func validateIRCIdentity(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \r\n\x00") {
		return fmt.Errorf("irc %s is required and cannot contain spaces or control characters", name)
	}
	return nil
}
