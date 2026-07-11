package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	apnsapi "github.com/gongahkia/onibi/internal/apns"
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
	if mode == "apns" {
		if _, err := apnsapi.New(apnsConfigFromOptions(opts.APNs)); err != nil {
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
		WebAddr:                 envProviderActionWebAddr(mode, opts, cfg.Web.ListenAddr),
		Matrix:                  opts.Matrix,
		Slack:                   opts.Slack,
		Discord:                 opts.Discord,
		Zulip:                   opts.Zulip,
		IRC:                     opts.IRC,
		Signal:                  opts.Signal,
		Pushover:                opts.Pushover,
		Ntfy:                    opts.Ntfy,
		Gotify:                  opts.Gotify,
		APNs:                    opts.APNs,
		SMS:                     opts.SMS,
		Email:                   opts.Email,
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
	Zulip    daemon.ZulipOptions
	IRC      daemon.IRCOptions
	Signal   daemon.SignalOptions
	Pushover daemon.PushoverOptions
	Ntfy     daemon.NtfyOptions
	Gotify   daemon.GotifyOptions
	APNs     daemon.APNsOptions
	SMS      daemon.SMSOptions
	Email    daemon.EmailOptions
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
			OwnerDeviceID:  strings.TrimSpace(os.Getenv("ONIBI_MATRIX_OWNER_DEVICE_ID")),
			AllowEncrypted: envBool("ONIBI_MATRIX_ALLOW_ENCRYPTED"),
			SASVerified:    envBool("ONIBI_MATRIX_SAS_VERIFIED"),
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
	case "zulip":
		opts.Zulip = daemon.ZulipOptions{
			BaseURL:     envRequired("ONIBI_ZULIP_URL"),
			Email:       envRequired("ONIBI_ZULIP_EMAIL"),
			APIKey:      envRequired("ONIBI_ZULIP_API_KEY"),
			Stream:      envRequired("ONIBI_ZULIP_STREAM"),
			TopicPrefix: strings.TrimSpace(os.Getenv("ONIBI_ZULIP_TOPIC_PREFIX")),
			OwnerEmail:  strings.TrimSpace(os.Getenv("ONIBI_ZULIP_OWNER_EMAIL")),
		}
		if opts.Zulip.BaseURL == "" || opts.Zulip.Email == "" || opts.Zulip.APIKey == "" || opts.Zulip.Stream == "" {
			return opts, "", fmt.Errorf("zulip requires ONIBI_ZULIP_URL, ONIBI_ZULIP_EMAIL, ONIBI_ZULIP_API_KEY, ONIBI_ZULIP_STREAM")
		}
		return opts, "Zulip", nil
	case "irc":
		opts.IRC = daemon.IRCOptions{
			Addr:      strings.TrimSpace(os.Getenv("ONIBI_IRC_ADDR")),
			Nick:      envRequired("ONIBI_IRC_NICK"),
			Username:  strings.TrimSpace(os.Getenv("ONIBI_IRC_USERNAME")),
			Password:  envRequired("ONIBI_IRC_PASSWORD"),
			OwnerNick: envRequired("ONIBI_IRC_OWNER_NICK"),
			Plaintext: envBool("ONIBI_IRC_PLAINTEXT"),
		}
		if opts.IRC.Nick == "" || opts.IRC.Password == "" || opts.IRC.OwnerNick == "" {
			return opts, "", fmt.Errorf("irc requires ONIBI_IRC_NICK, ONIBI_IRC_PASSWORD, ONIBI_IRC_OWNER_NICK")
		}
		return opts, "IRC", nil
	case "signal":
		opts.Signal = daemon.SignalOptions{
			RPCURL:     envRequired("ONIBI_SIGNAL_RPC_URL"),
			Account:    envRequired("ONIBI_SIGNAL_ACCOUNT"),
			Recipients: signalRecipientsFromEnv(),
			GroupID:    strings.TrimSpace(os.Getenv("ONIBI_SIGNAL_GROUP_ID")),
			Owner:      strings.TrimSpace(os.Getenv("ONIBI_SIGNAL_OWNER")),
		}
		if opts.Signal.RPCURL == "" || opts.Signal.Account == "" || (len(opts.Signal.Recipients) == 0 && opts.Signal.GroupID == "") {
			return opts, "", fmt.Errorf("signal requires ONIBI_SIGNAL_RPC_URL, ONIBI_SIGNAL_ACCOUNT, and ONIBI_SIGNAL_RECIPIENT or ONIBI_SIGNAL_GROUP_ID")
		}
		return opts, "Signal", nil
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
	case "apns":
		opts.APNs = daemon.APNsOptions{
			KeyPath:     envRequired("ONIBI_APNS_KEY_PATH"),
			KeyID:       envRequired("ONIBI_APNS_KEY_ID"),
			TeamID:      envRequired("ONIBI_APNS_TEAM_ID"),
			Topic:       envRequired("ONIBI_APNS_TOPIC"),
			DeviceToken: envRequired("ONIBI_APNS_DEVICE_TOKEN"),
			Environment: strings.TrimSpace(os.Getenv("ONIBI_APNS_ENV")),
		}
		if opts.APNs.KeyPath == "" || opts.APNs.KeyID == "" || opts.APNs.TeamID == "" || opts.APNs.Topic == "" || opts.APNs.DeviceToken == "" {
			return opts, "", fmt.Errorf("apns requires ONIBI_APNS_KEY_PATH, ONIBI_APNS_KEY_ID, ONIBI_APNS_TEAM_ID, ONIBI_APNS_TOPIC, ONIBI_APNS_DEVICE_TOKEN")
		}
		return opts, "APNs", nil
	case "sms":
		opts.SMS = daemon.SMSOptions{
			AccountSID:          envRequired("ONIBI_TWILIO_ACCOUNT_SID"),
			AuthToken:           envRequired("ONIBI_TWILIO_AUTH_TOKEN"),
			From:                strings.TrimSpace(os.Getenv("ONIBI_TWILIO_FROM")),
			MessagingServiceSID: strings.TrimSpace(os.Getenv("ONIBI_TWILIO_MESSAGING_SERVICE_SID")),
			To:                  envRequired("ONIBI_SMS_TO"),
			ActionBaseURL:       envRequired("ONIBI_SMS_ACTION_BASE_URL"),
		}
		if opts.SMS.AccountSID == "" || opts.SMS.AuthToken == "" || opts.SMS.To == "" || opts.SMS.ActionBaseURL == "" || (opts.SMS.From == "" && opts.SMS.MessagingServiceSID == "") {
			return opts, "", fmt.Errorf("sms requires ONIBI_TWILIO_ACCOUNT_SID, ONIBI_TWILIO_AUTH_TOKEN, ONIBI_SMS_TO, ONIBI_SMS_ACTION_BASE_URL, and ONIBI_TWILIO_FROM or ONIBI_TWILIO_MESSAGING_SERVICE_SID")
		}
		return opts, "SMS", nil
	case "email":
		opts.Email = daemon.EmailOptions{
			Addr:          envRequired("ONIBI_SMTP_ADDR"),
			Host:          strings.TrimSpace(os.Getenv("ONIBI_SMTP_HOST")),
			Username:      strings.TrimSpace(os.Getenv("ONIBI_SMTP_USERNAME")),
			Password:      strings.TrimSpace(os.Getenv("ONIBI_SMTP_PASSWORD")),
			From:          envRequired("ONIBI_EMAIL_FROM"),
			To:            envRequired("ONIBI_EMAIL_TO"),
			ActionBaseURL: envRequired("ONIBI_EMAIL_ACTION_BASE_URL"),
		}
		if opts.Email.Addr == "" || opts.Email.From == "" || opts.Email.To == "" || opts.Email.ActionBaseURL == "" {
			return opts, "", fmt.Errorf("email requires ONIBI_SMTP_ADDR, ONIBI_EMAIL_FROM, ONIBI_EMAIL_TO, ONIBI_EMAIL_ACTION_BASE_URL")
		}
		return opts, "Email", nil
	default:
		return opts, "", fmt.Errorf("unsupported env provider %q", mode)
	}
}

func isEnvChatTransport(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "matrix", "slack", "discord", "zulip", "irc", "signal":
		return true
	default:
		return false
	}
}

func isNotifyTransport(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "pushover", "ntfy", "gotify", "apns", "sms", "email":
		return true
	default:
		return false
	}
}

func envProviderActionWebAddr(mode string, opts envProviderOptions, listenAddr string) string {
	if !isNotifyTransport(mode) {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ntfy":
		if strings.TrimSpace(opts.Ntfy.ActionBaseURL) != "" {
			return listenAddr
		}
	case "gotify":
		if strings.TrimSpace(opts.Gotify.ActionBaseURL) != "" {
			return listenAddr
		}
	case "sms":
		if strings.TrimSpace(opts.SMS.ActionBaseURL) != "" {
			return listenAddr
		}
	case "email":
		if strings.TrimSpace(opts.Email.ActionBaseURL) != "" {
			return listenAddr
		}
	}
	return ""
}

func apnsConfigFromOptions(opts daemon.APNsOptions) apnsapi.Config {
	return apnsapi.Config{KeyPath: opts.KeyPath, KeyID: opts.KeyID, TeamID: opts.TeamID, Topic: opts.Topic, Environment: opts.Environment}
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

func signalRecipientsFromEnv() []string {
	if v := strings.TrimSpace(os.Getenv("ONIBI_SIGNAL_RECIPIENTS")); v != "" {
		return splitCSV(v)
	}
	return splitCSV(os.Getenv("ONIBI_SIGNAL_RECIPIENT"))
}
