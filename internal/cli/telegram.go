package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/spf13/cobra"
)

func telegramCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "telegram", Short: "Configure the Telegram command center", RunE: runTelegramStatus}
	setup := &cobra.Command{Use: "setup", Short: "Store and validate a BotFather token", RunE: runTelegramSetup}
	setup.Flags().String("token", "", "BotFather token")
	setup.Flags().Bool("no-check", false, "skip Telegram getMe validation")
	status := &cobra.Command{Use: "status", Short: "Show Telegram setup state", RunE: runTelegramStatus}
	status.Flags().Bool("check", false, "validate token with Telegram")
	status.Flags().Bool("json", false, "print JSON")
	disable := &cobra.Command{Use: "disable", Short: "Remove token and pairing", RunE: runTelegramDisable}
	cmd.AddCommand(setup, status, disable)
	return cmd
}
func runTelegramSetup(cmd *cobra.Command, _ []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	token, _ := cmd.Flags().GetString("token")
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("--token required")
	}
	skip, _ := cmd.Flags().GetBool("no-check")
	if !telegram.ValidBotToken(token) {
		return errors.New("token does not look like a BotFather token")
	}
	var bot telegram.User
	if !skip {
		bot, err = telegram.NewClient(token).GetMe(cmd.Context())
		if err != nil {
			return err
		}
	}
	secretsStore, err := telegramSecrets(paths)
	if err != nil {
		return err
	}
	if err := secretsStore.Set(daemon.TelegramSecretBotToken, token); err != nil {
		return err
	}
	chat, user := telegramBinding(cmd.Context(), db)
	if chat == 0 || user == 0 {
		code, err := ensurePairCode(cmd.Context(), db)
		if err != nil {
			return err
		}
		if skip {
			fmt.Fprintf(cmd.OutOrStdout(), "Pair: send /start %s while onibi start is running.\n", code)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Bot @%s stored. Pair: send /start %s while onibi start is running.\n", bot.Username, code)
		}
	}
	return nil
}
func runTelegramStatus(cmd *cobra.Command, _ []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	token, err := telegramToken(paths)
	if err != nil {
		return err
	}
	chat, user := telegramBinding(cmd.Context(), db)
	check, _ := cmd.Flags().GetBool("check")
	report := map[string]any{"token": token != "", "owner_paired": chat != 0 && user != 0, "owner_chat_id": chat, "owner_user_id": user, "e2e_encrypted": false, "surface": "Telegram only"}
	if check && token != "" {
		bot, err := telegram.NewClient(token).GetMe(cmd.Context())
		report["token_valid"] = err == nil
		if err != nil {
			report["check_error"] = err.Error()
		} else {
			report["bot"] = "@" + bot.Username
		}
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "token=%t\nowner_paired=%t\n", token != "", chat != 0 && user != 0)
	if chat == 0 || user == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "next=onibi telegram setup; onibi start")
	}
	return nil
}
func runTelegramDisable(cmd *cobra.Command, _ []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	secretsStore, err := telegramSecrets(paths)
	if err != nil {
		return err
	}
	_ = secretsStore.Delete(daemon.TelegramSecretBotToken)
	for _, key := range []string{daemon.TelegramKVOwnerChatID, daemon.TelegramKVOwnerUserID, daemon.TelegramKVPairCode} {
		_ = db.KVDel(cmd.Context(), key)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Telegram disabled.")
	return nil
}

var _ = strconv.IntSize
