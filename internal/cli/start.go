package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/spf13/cobra"
)

func startCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "start", Short: "Start the Telegram command center", Args: cobra.NoArgs, RunE: runStart}
	cmd.Flags().String("log-file", "", "write daemon logs to this file")
	return cmd
}
func runStart(cmd *cobra.Command, _ []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	if err := render.ValidateFont(cfg.Screen.Font, cfg.Screen.FontPath); err != nil {
		return fmt.Errorf("screen font: %w", err)
	}
	token, err := telegramToken(paths)
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("Telegram bot token missing; run onibi telegram setup")
	}
	ownerChat, ownerUser := telegramBinding(cmd.Context(), db)
	pair := ""
	if ownerChat == 0 || ownerUser == 0 {
		pair, err = ensurePairCode(cmd.Context(), db)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pair: send /start %s to your bot.\n", pair)
	}
	level := slog.LevelInfo
	debug, _ := cmd.Root().PersistentFlags().GetBool("debug")
	if debug {
		level = slog.LevelDebug
	}
	logPath, _ := cmd.Flags().GetString("log-file")
	if logPath == "" {
		logPath = filepath.Join(paths.LogDir, "onibi.log")
	}
	file, err := logging.OpenRotating(logPath, logging.DefaultMaxBytes, logging.DefaultBackups)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := io.MultiWriter(cmd.ErrOrStderr(), file)
	logging.SetSecrets(token)
	logger := logging.New(writer, level)
	ctx, stop := signal.NotifyContext(contextOrBackground(cmd.Context()), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	d := daemon.New(daemon.Options{Paths: paths, DB: db, Log: logger, ApprovalTTL: cfg.Daemon.ApprovalTimeout.Std(), ClaudeQuestionTimeout: cfg.Daemon.ClaudeQuestionTimeout.Std(), ApprovalSweepInterval: cfg.Daemon.ApprovalSweepInterval.Std(), ApprovalMaxSubscribers: cfg.Daemon.MaxSubscribers, OutputBufferSize: cfg.Daemon.OutputBufferBytes, LivenessInterval: cfg.Daemon.LivenessInterval.Std(), UploadTTL: cfg.Daemon.UploadTTL.Std(), UploadMaxBytes: cfg.Daemon.UploadMaxBytes, ShellDefault: cfg.Shell.Default, ShellLogin: cfg.Shell.Login, ScreenFont: cfg.Screen.Font, ScreenFontPath: cfg.Screen.FontPath, TelegramToken: token, TelegramOwnerID: ownerChat, TelegramOwnerUserID: ownerUser, TelegramPair: pair})
	return d.Run(ctx)
}
func telegramBinding(ctx context.Context, db interface {
	KVGetString(context.Context, string) (string, bool, error)
}) (int64, int64) {
	chat, _, _ := db.KVGetString(ctx, daemon.TelegramKVOwnerChatID)
	user, _, _ := db.KVGetString(ctx, daemon.TelegramKVOwnerUserID)
	chatID, _ := strconv.ParseInt(chat, 10, 64)
	userID, _ := strconv.ParseInt(user, 10, 64)
	return chatID, userID
}
func ensurePairCode(ctx context.Context, db interface {
	KVGetString(context.Context, string) (string, bool, error)
	KVSetString(context.Context, string, string) error
}) (string, error) {
	if code, ok, err := db.KVGetString(ctx, daemon.TelegramKVPairCode); err == nil && ok && code != "" {
		return code, nil
	}
	code, err := daemon.NewTelegramPairCode()
	if err != nil {
		return "", err
	}
	return code, db.KVSetString(ctx, daemon.TelegramKVPairCode, code)
}
