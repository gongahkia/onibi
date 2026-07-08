package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	gotifyapi "github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/store"
)

func runEnvProviderUp(cmd *cobra.Command, paths config.Paths, db *store.DB, cfg config.Config, logger *slog.Logger, started time.Time, shellCWD string) error {
	mode := strings.ToLower(strings.TrimSpace(cfg.Transport.Mode))
	opts, label, err := providerOptionsFromEnv(mode)
	if err != nil {
		return err
	}
	if mode == "gotify" {
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		if err := gotifyapi.New(opts.Gotify.BaseURL, opts.Gotify.AppToken, opts.Gotify.ClientToken).Validate(ctx); err != nil {
			return err
		}
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
		Matrix:                  opts.Matrix,
		Slack:                   opts.Slack,
		Discord:                 opts.Discord,
		Pushover:                opts.Pushover,
		Ntfy:                    opts.Ntfy,
		Gotify:                  opts.Gotify,
		ProviderOutput:          daemonProviderOutputPolicy(cfg),
		ProviderOutputOverrides: daemonProviderOutputOverrides(cfg),
		UpdateAuto:              cfg.Update.Auto,
		UpdateChannel:           cfg.Update.Channel,
		SkipRestore:             true,
	})
	var session *daemon.Session
	if isEnvChatTransport(mode) {
		var err error
		session, err = startManagedWebPairShell(cmd.Context(), d, cfg, shellCWD, logger)
		if err != nil {
			return err
		}
		defer cleanupManagedWebPairShell(logger, d, session.ID, session.TmuxTarget)
	}
	if quiet(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), label)
	} else {
		printCLIHeader(cmd, label)
		if session != nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Session:", session.ID)
			fmt.Fprintln(cmd.OutOrStdout(), "Text the provider to send input. Press Ctrl-C to stop.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Approval notifications only. Press Ctrl-C to stop.")
		}
	}
	logger.Info("onibi provider ready", "transport", mode, "uptime_ms", time.Since(started).Milliseconds())
	return d.Run(cmd.Context())
}

type envProviderOptions struct {
	Matrix   daemon.MatrixOptions
	Slack    daemon.SlackOptions
	Discord  daemon.DiscordOptions
	Pushover daemon.PushoverOptions
	Ntfy     daemon.NtfyOptions
	Gotify   daemon.GotifyOptions
}

func providerOptionsFromEnv(mode string) (envProviderOptions, string, error) {
	var opts envProviderOptions
	switch mode {
	case "matrix":
		opts.Matrix = daemon.MatrixOptions{
			Homeserver:     envRequired("ONIBI_MATRIX_HOMESERVER"),
			AccessToken:    envRequired("ONIBI_MATRIX_ACCESS_TOKEN"),
			RoomID:         envRequired("ONIBI_MATRIX_ROOM_ID"),
			OwnerUserID:    strings.TrimSpace(os.Getenv("ONIBI_MATRIX_OWNER_USER_ID")),
			AllowEncrypted: envBool("ONIBI_MATRIX_ALLOW_ENCRYPTED"),
		}
		if opts.Matrix.Homeserver == "" || opts.Matrix.AccessToken == "" || opts.Matrix.RoomID == "" {
			return opts, "", fmt.Errorf("matrix requires ONIBI_MATRIX_HOMESERVER, ONIBI_MATRIX_ACCESS_TOKEN, ONIBI_MATRIX_ROOM_ID")
		}
		return opts, "Matrix", nil
	case "slack":
		opts.Slack = daemon.SlackOptions{
			AppToken:        envRequired("ONIBI_SLACK_APP_TOKEN"),
			BotToken:        envRequired("ONIBI_SLACK_BOT_TOKEN"),
			AllowedIDs:      splitCSV(os.Getenv("ONIBI_SLACK_ALLOWED_CHANNELS")),
			AllowedDMUsers:  splitCSV(os.Getenv("ONIBI_SLACK_ALLOWED_DM_USERS")),
			ApprovalChannel: strings.TrimSpace(os.Getenv("ONIBI_SLACK_APPROVAL_CHANNEL")),
		}
		if opts.Slack.AppToken == "" || opts.Slack.BotToken == "" {
			return opts, "", fmt.Errorf("slack requires ONIBI_SLACK_APP_TOKEN and ONIBI_SLACK_BOT_TOKEN")
		}
		return opts, "Slack", nil
	case "discord":
		opts.Discord = daemon.DiscordOptions{
			Token:      envRequired("ONIBI_DISCORD_TOKEN"),
			GatewayURL: strings.TrimSpace(os.Getenv("ONIBI_DISCORD_GATEWAY_URL")),
			AllowedIDs: splitCSV(os.Getenv("ONIBI_DISCORD_ALLOWED_IDS")),
		}
		if opts.Discord.Token == "" {
			return opts, "", fmt.Errorf("discord requires ONIBI_DISCORD_TOKEN")
		}
		return opts, "Discord", nil
	case "pushover":
		opts.Pushover = daemon.PushoverOptions{Token: envRequired("ONIBI_PUSHOVER_TOKEN"), UserKey: envRequired("ONIBI_PUSHOVER_USER_KEY")}
		if opts.Pushover.Token == "" || opts.Pushover.UserKey == "" {
			return opts, "", fmt.Errorf("pushover requires ONIBI_PUSHOVER_TOKEN and ONIBI_PUSHOVER_USER_KEY")
		}
		return opts, "Pushover", nil
	case "ntfy":
		opts.Ntfy = daemon.NtfyOptions{BaseURL: strings.TrimSpace(os.Getenv("ONIBI_NTFY_BASE_URL")), Topic: envRequired("ONIBI_NTFY_TOPIC"), Token: strings.TrimSpace(os.Getenv("ONIBI_NTFY_TOKEN")), ActionBaseURL: strings.TrimSpace(os.Getenv("ONIBI_NTFY_ACTION_BASE_URL"))}
		if opts.Ntfy.Topic == "" {
			return opts, "", fmt.Errorf("ntfy requires ONIBI_NTFY_TOPIC")
		}
		return opts, "ntfy", nil
	case "gotify":
		opts.Gotify = daemon.GotifyOptions{BaseURL: envRequired("ONIBI_GOTIFY_URL"), AppToken: envRequired("ONIBI_GOTIFY_APP_TOKEN"), ClientToken: strings.TrimSpace(os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN")), ActionBaseURL: strings.TrimSpace(os.Getenv("ONIBI_GOTIFY_ACTION_BASE_URL"))}
		if opts.Gotify.BaseURL == "" || opts.Gotify.AppToken == "" {
			return opts, "", fmt.Errorf("gotify requires ONIBI_GOTIFY_URL and ONIBI_GOTIFY_APP_TOKEN")
		}
		return opts, "Gotify", nil
	default:
		return opts, "", fmt.Errorf("unsupported env provider %q", mode)
	}
}

func isEnvChatTransport(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "matrix", "slack", "discord":
		return true
	default:
		return false
	}
}

func isNotifyTransport(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "pushover", "ntfy", "gotify":
		return true
	default:
		return false
	}
}

func envRequired(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
