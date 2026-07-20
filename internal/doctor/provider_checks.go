package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
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

var (
	newTelegramProviderClient = func(token string) *telegram.Client {
		return telegram.NewClient(token)
	}
)

func Providers(ctx context.Context, opts Options) ProviderReport {
	pa := providerAudit(ctx, opts.Paths)
	rows := []ProviderRow{
		providerTelegram(ctx, opts, pa),
		providerMatrix(ctx, opts, pa),
		providerSlack(ctx, opts, pa),
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
		for _, name := range []string{"telegram", "matrix", "slack"} {
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
