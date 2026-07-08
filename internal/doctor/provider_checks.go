package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/apns"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pushover"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/zulip"
)

const (
	ReachableYes     = "yes"
	ReachableNo      = "no"
	ReachableSkipped = "skipped"
)

type ProviderReport struct {
	Providers []ProviderRow `json:"providers"`
}

type ProviderRow struct {
	Name               string   `json:"name"`
	Configured         bool     `json:"configured"`
	Reachable          string   `json:"reachable"`
	LastAuditTimestamp string   `json:"last_audit_timestamp,omitempty"`
	Detail             string   `json:"detail,omitempty"`
	Fix                []string `json:"fix,omitempty"`
}

type apnsPusher interface {
	PushApproval(context.Context, apns.PushRequest) (apns.PushResult, error)
}

var (
	newTelegramProviderClient = func(token string) *telegram.Client {
		return telegram.NewClient(token)
	}
	newPushoverClient = func(token, userKey string) *pushover.Client {
		return pushover.New(token, userKey)
	}
	newNtfyClient = func(baseURL, topic, token string) *ntfy.Client {
		return ntfy.New(baseURL, topic, token)
	}
	newGotifyClient = func(baseURL, appToken, clientToken string) *gotify.Client {
		return gotify.New(baseURL, appToken, clientToken)
	}
	newAPNsProviderClient = func(cfg apns.Config) (apnsPusher, error) {
		return apns.New(cfg)
	}
	newZulipClient = func(baseURL, email, apiKey string) *zulip.Client {
		return zulip.New(baseURL, email, apiKey)
	}
)

func Providers(ctx context.Context, opts Options) ProviderReport {
	pa := providerAudit(ctx, opts.Paths)
	rows := []ProviderRow{
		providerTelegram(ctx, opts, pa),
		providerMatrix(ctx, opts, pa),
		providerSlack(ctx, opts, pa),
		providerDiscord(ctx, opts, pa),
		providerZulip(ctx, opts, pa),
		providerPushover(ctx, opts, pa),
		providerNtfy(ctx, opts, pa),
		providerGotify(ctx, opts, pa),
		providerAPNs(ctx, opts, pa),
	}
	return ProviderReport{Providers: rows}
}

func providerTelegram(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("telegram", pa)
	token, tokenOK, source, err := providerTelegramToken(ctx, opts)
	ownerOK, ownerErr := telegramOwnerPaired(ctx, opts.Paths)
	row.Configured = tokenOK && ownerOK
	switch {
	case err != nil:
		row.Detail = "token lookup failed: " + err.Error()
	case ownerErr != nil:
		row.Detail = "owner lookup failed: " + ownerErr.Error()
	case !tokenOK && !ownerOK:
		row.Detail = "missing bot token and owner_chat_id"
	case !tokenOK:
		row.Detail = "missing bot token"
	case !ownerOK:
		row.Detail = "missing owner_chat_id"
	default:
		row.Detail = "token=" + source + " owner_chat_id=present"
	}
	if row.Configured && !opts.Offline {
		withProviderTimeout(ctx, func(ctx context.Context) {
			user, err := newTelegramProviderClient(token).GetMe(ctx)
			if err != nil {
				row.Reachable = ReachableNo
				row.Detail = "getMe failed: " + err.Error()
				return
			}
			row.Reachable = ReachableYes
			row.Detail = "@" + providerValueOrDefault(user.Username, fmt.Sprint(user.ID))
		})
	}
	if !row.Configured {
		row.Fix = []string{"run onibi telegram setup", "run onibi up --transport=telegram and send the printed /start code"}
	}
	return row
}

func providerMatrix(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("matrix", pa)
	missing := missingEnv("ONIBI_MATRIX_HOMESERVER", "ONIBI_MATRIX_ACCESS_TOKEN", "ONIBI_MATRIX_ROOM_ID")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_MATRIX_HOMESERVER, ONIBI_MATRIX_ACCESS_TOKEN, ONIBI_MATRIX_ROOM_ID", "run onibi up --transport=matrix"}
		return row
	}
	row.Detail = "env present"
	if opts.Offline {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		roomID := strings.TrimSpace(os.Getenv("ONIBI_MATRIX_ROOM_ID"))
		rooms, err := matrix.New(os.Getenv("ONIBI_MATRIX_HOMESERVER"), os.Getenv("ONIBI_MATRIX_ACCESS_TOKEN")).JoinedRooms(ctx)
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = "joined_rooms failed: " + err.Error()
			return
		}
		if !containsString(rooms.JoinedRooms, roomID) {
			row.Reachable = ReachableNo
			row.Detail = "account not joined to " + roomID
			return
		}
		row.Reachable = ReachableYes
		row.Detail = "joined " + roomID
	})
	return row
}

func providerSlack(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("slack", pa)
	missing := missingEnv("ONIBI_SLACK_APP_TOKEN", "ONIBI_SLACK_BOT_TOKEN")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_SLACK_APP_TOKEN and ONIBI_SLACK_BOT_TOKEN", "set ONIBI_SLACK_APPROVAL_CHANNEL or ONIBI_SLACK_ALLOWED_CHANNELS for approvals"}
		return row
	}
	row.Detail = "env present"
	if opts.Offline {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		c := newSlackClient(os.Getenv("ONIBI_SLACK_APP_TOKEN"), os.Getenv("ONIBI_SLACK_BOT_TOKEN"))
		auth, err := c.AuthTest(ctx)
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = "auth.test failed: " + err.Error()
			return
		}
		if _, err := c.OpenSocket(ctx); err != nil {
			row.Reachable = ReachableNo
			row.Detail = "socket mode failed: " + err.Error()
			return
		}
		row.Reachable = ReachableYes
		row.Detail = "team=" + auth.TeamID
	})
	return row
}

func providerDiscord(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("discord", pa)
	missing := missingEnv("ONIBI_DISCORD_TOKEN")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_DISCORD_TOKEN", "set ONIBI_DISCORD_CHANNEL_ID for channel checks; run onibi discord register for slash commands"}
		return row
	}
	row.Detail = "env present"
	if opts.Offline {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		c := newDiscordClient(os.Getenv("ONIBI_DISCORD_TOKEN"))
		app, err := c.CurrentApplication(ctx)
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = "application check failed: " + err.Error()
			return
		}
		if channel := strings.TrimSpace(os.Getenv("ONIBI_DISCORD_CHANNEL_ID")); channel != "" {
			if _, err := c.Channel(ctx, channel); err != nil {
				row.Reachable = ReachableNo
				row.Detail = "channel check failed: " + err.Error()
				return
			}
			row.Detail = "app=" + app.ID + " channel=" + channel
		} else {
			row.Detail = "app=" + app.ID
		}
		row.Reachable = ReachableYes
	})
	return row
}

func providerZulip(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("zulip", pa)
	missing := missingEnv("ONIBI_ZULIP_URL", "ONIBI_ZULIP_EMAIL", "ONIBI_ZULIP_API_KEY", "ONIBI_ZULIP_STREAM")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_ZULIP_URL, ONIBI_ZULIP_EMAIL, ONIBI_ZULIP_API_KEY, ONIBI_ZULIP_STREAM", "run onibi up --transport=zulip"}
		return row
	}
	row.Detail = "env present"
	if opts.Offline {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		q, err := newZulipClient(os.Getenv("ONIBI_ZULIP_URL"), os.Getenv("ONIBI_ZULIP_EMAIL"), os.Getenv("ONIBI_ZULIP_API_KEY")).RegisterQueue(ctx, zulip.QueueOptions{EventTypes: []string{"message"}, Narrow: [][]string{{"channel", os.Getenv("ONIBI_ZULIP_STREAM")}}})
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = "event queue failed: " + err.Error()
			return
		}
		_ = newZulipClient(os.Getenv("ONIBI_ZULIP_URL"), os.Getenv("ONIBI_ZULIP_EMAIL"), os.Getenv("ONIBI_ZULIP_API_KEY")).DeleteQueue(ctx, q.QueueID)
		row.Reachable = ReachableYes
		row.Detail = "event queue ok"
	})
	return row
}

func providerPushover(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("pushover", pa)
	missing := missingEnv("ONIBI_PUSHOVER_TOKEN", "ONIBI_PUSHOVER_USER_KEY")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_PUSHOVER_TOKEN and ONIBI_PUSHOVER_USER_KEY", "run onibi up --transport=pushover"}
		return row
	}
	row.Detail = "env present; set ONIBI_DOCTOR_LIVE=1 for send probe"
	if opts.Offline || !doctorLiveProbe() {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		_, err := newPushoverClient(os.Getenv("ONIBI_PUSHOVER_TOKEN"), os.Getenv("ONIBI_PUSHOVER_USER_KEY")).Send(ctx, pushover.MessageOptions{Title: "Onibi doctor", Message: "onibi doctor pushover probe", Priority: -2})
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = "send probe failed: " + err.Error()
			return
		}
		row.Reachable = ReachableYes
		row.Detail = "send probe ok"
	})
	return row
}

func providerNtfy(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("ntfy", pa)
	topic := strings.TrimSpace(os.Getenv("ONIBI_NTFY_TOPIC"))
	if topic == "" {
		row.Detail = "missing ONIBI_NTFY_TOPIC"
		row.Fix = []string{"set ONIBI_NTFY_TOPIC to a 20+ character random secret", "optionally set ONIBI_NTFY_BASE_URL and ONIBI_NTFY_TOKEN"}
		return row
	}
	if err := ntfy.ValidateTopicSecret(topic); err != nil {
		row.Detail = "topic weak: " + err.Error()
		row.Fix = []string{"replace ONIBI_NTFY_TOPIC with a 20+ character random secret"}
		return row
	}
	row.Configured = true
	row.Detail = "topic valid; set ONIBI_DOCTOR_LIVE=1 for publish probe"
	if opts.Offline || !doctorLiveProbe() {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		if err := newNtfyClient(os.Getenv("ONIBI_NTFY_BASE_URL"), topic, os.Getenv("ONIBI_NTFY_TOKEN")).Publish(ctx, ntfy.Message{Title: "Onibi doctor", Body: "onibi doctor ntfy probe"}); err != nil {
			row.Reachable = ReachableNo
			row.Detail = "publish probe failed: " + err.Error()
			return
		}
		row.Reachable = ReachableYes
		row.Detail = "publish probe ok"
	})
	return row
}

func providerGotify(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("gotify", pa)
	missing := missingEnv("ONIBI_GOTIFY_URL", "ONIBI_GOTIFY_APP_TOKEN")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_GOTIFY_URL and ONIBI_GOTIFY_APP_TOKEN", "optionally set ONIBI_GOTIFY_CLIENT_TOKEN for read-side validation"}
		return row
	}
	row.Detail = "env present"
	clientToken := strings.TrimSpace(os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN"))
	if opts.Offline {
		return row
	}
	if clientToken == "" && !doctorLiveProbe() {
		row.Detail = "env present; set ONIBI_GOTIFY_CLIENT_TOKEN or ONIBI_DOCTOR_LIVE=1 for reachability"
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		c := newGotifyClient(os.Getenv("ONIBI_GOTIFY_URL"), os.Getenv("ONIBI_GOTIFY_APP_TOKEN"), clientToken)
		if clientToken != "" {
			if err := c.Validate(ctx); err != nil {
				row.Reachable = ReachableNo
				row.Detail = "validate failed: " + err.Error()
				return
			}
			row.Reachable = ReachableYes
			row.Detail = "client token validation ok"
			return
		}
		if err := c.Send(ctx, gotify.Message{Title: "Onibi doctor", Message: "onibi doctor gotify probe", Priority: 1}); err != nil {
			row.Reachable = ReachableNo
			row.Detail = "send probe failed: " + err.Error()
			return
		}
		row.Reachable = ReachableYes
		row.Detail = "send probe ok"
	})
	return row
}

func providerAPNs(ctx context.Context, opts Options, pa map[string]string) ProviderRow {
	row := providerRow("apns", pa)
	missing := missingEnv("ONIBI_APNS_KEY_PATH", "ONIBI_APNS_KEY_ID", "ONIBI_APNS_TEAM_ID", "ONIBI_APNS_TOPIC", "ONIBI_APNS_DEVICE_TOKEN")
	row.Configured = len(missing) == 0
	if !row.Configured {
		row.Detail = "missing " + strings.Join(missing, ", ")
		row.Fix = []string{"set ONIBI_APNS_KEY_PATH, ONIBI_APNS_KEY_ID, ONIBI_APNS_TEAM_ID, ONIBI_APNS_TOPIC, ONIBI_APNS_DEVICE_TOKEN", "run onibi up --transport=apns"}
		return row
	}
	cfg := apns.Config{KeyPath: os.Getenv("ONIBI_APNS_KEY_PATH"), KeyID: os.Getenv("ONIBI_APNS_KEY_ID"), TeamID: os.Getenv("ONIBI_APNS_TEAM_ID"), Topic: os.Getenv("ONIBI_APNS_TOPIC"), Environment: os.Getenv("ONIBI_APNS_ENV")}
	if err := cfg.Validate(); err != nil {
		row.Detail = err.Error()
		return row
	}
	row.Detail = "env present; set ONIBI_DOCTOR_LIVE=1 for APNs send probe"
	if opts.Offline || !doctorLiveProbe() {
		return row
	}
	withProviderTimeout(ctx, func(ctx context.Context) {
		c, err := newAPNsProviderClient(cfg)
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = "client failed: " + err.Error()
			return
		}
		result, err := c.PushApproval(ctx, apns.PushRequest{DeviceToken: os.Getenv("ONIBI_APNS_DEVICE_TOKEN"), Title: "Onibi doctor", Body: "onibi doctor apns probe", ApprovalID: "doctor", TTL: 30 * time.Second})
		if err != nil {
			row.Reachable = ReachableNo
			row.Detail = fmt.Sprintf("send probe failed: status=%d reason=%s err=%s", result.StatusCode, result.Reason, err.Error())
			return
		}
		row.Reachable = ReachableYes
		row.Detail = "send probe ok"
	})
	return row
}

func providerRow(name string, pa map[string]string) ProviderRow {
	return ProviderRow{Name: name, Reachable: ReachableSkipped, LastAuditTimestamp: pa[name]}
}

func providerTelegramToken(ctx context.Context, opts Options) (string, bool, string, error) {
	if token := strings.TrimSpace(os.Getenv("ONIBI_TELEGRAM_TOKEN")); token != "" {
		return token, true, "env", nil
	}
	st, err := secrets.Open(secrets.Options{EnvFallbackPath: opts.Paths.EnvFile, PreferDotenv: opts.PreferDotenv})
	if err != nil {
		return "", false, "", err
	}
	token, ok, err := st.GetWithTimeout(ctx, daemon.TelegramSecretBotToken, time.Second)
	if err != nil {
		return "", false, "", err
	}
	return token, ok, string(st.Backend()), nil
}

func telegramOwnerPaired(ctx context.Context, paths config.Paths) (bool, error) {
	db, err := openProviderDB(paths)
	if err != nil {
		if errorsIsMissingState(err) {
			return false, nil
		}
		return false, err
	}
	defer db.Close()
	_, ok, err := db.KVGetString(ctx, daemon.TelegramKVOwnerChatID)
	return ok, err
}

func providerAudit(ctx context.Context, paths config.Paths) map[string]string {
	db, err := openProviderDB(paths)
	if err != nil {
		return map[string]string{}
	}
	defer db.Close()
	entries, err := db.AuditAll(ctx)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, e := range entries {
		for _, name := range []string{"telegram", "matrix", "slack", "discord", "zulip", "pushover", "ntfy", "gotify", "apns"} {
			if providerAuditMatch(name, e) {
				out[name] = e.TS.UTC().Format(time.RFC3339)
			}
		}
	}
	return out
}

func openProviderDB(paths config.Paths) (*store.DB, error) {
	if _, err := os.Stat(paths.DBFile); err != nil {
		return nil, err
	}
	return openStoreDB(paths.DBFile)
}

func providerAuditMatch(name string, e store.AuditEntry) bool {
	action := strings.ToLower(e.Action)
	detail := strings.ToLower(e.Detail)
	switch name {
	case "telegram":
		return (action == "approval.decided" && e.DecidedByChat != 0) || strings.Contains(action, "telegram") || strings.Contains(detail, "telegram")
	case "pushover":
		return strings.Contains(action, "notify.pushover")
	case "ntfy":
		return strings.Contains(action, "notify.ntfy")
	case "gotify":
		return strings.Contains(action, "notify.gotify")
	case "apns":
		return strings.Contains(action, "notify.apns")
	default:
		return strings.Contains(action, name) || strings.Contains(detail, "provider="+name)
	}
}

func withProviderTimeout(ctx context.Context, fn func(context.Context)) {
	child, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	fn(child)
}

func errorsIsMissingState(err error) bool {
	return os.IsNotExist(err) || strings.Contains(err.Error(), "store key not found")
}

func providerValueOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
