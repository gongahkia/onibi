package doctor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/apns"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/pushover"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

const providersTestTelegramToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestProvidersReportAllProvidersUnconfigured(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	want := []string{"telegram", "matrix", "slack", "discord", "zulip", "pushover", "ntfy", "gotify", "apns"}
	if len(report.Providers) != len(want) {
		t.Fatalf("providers = %#v", report.Providers)
	}
	for i, name := range want {
		row := report.Providers[i]
		if row.Name != name || row.Configured || row.Reachable != ReachableSkipped {
			t.Fatalf("row[%d] = %#v", i, row)
		}
	}
}

func TestProvidersConfiguredStatePerProvider(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	configureTelegramProvider(t, paths)
	configureEnvProviders(t)
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	for _, row := range report.Providers {
		if !row.Configured || row.Reachable != ReachableSkipped {
			t.Fatalf("%s row = %#v", row.Name, row)
		}
	}
}

func TestProvidersMissingDetailsPerProvider(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	want := map[string]string{
		"telegram": "missing bot token",
		"matrix":   "ONIBI_MATRIX_HOMESERVER",
		"slack":    "ONIBI_SLACK_APP_TOKEN",
		"discord":  "ONIBI_DISCORD_TOKEN",
		"zulip":    "ONIBI_ZULIP_URL",
		"pushover": "ONIBI_PUSHOVER_TOKEN",
		"ntfy":     "ONIBI_NTFY_TOPIC",
		"gotify":   "ONIBI_GOTIFY_URL",
		"apns":     "ONIBI_APNS_KEY_PATH",
	}
	for name, detail := range want {
		row := providerNamed(t, report, name)
		if row.Configured || !strings.Contains(row.Detail, detail) || len(row.Fix) == 0 {
			t.Fatalf("%s row = %#v", name, row)
		}
	}
}

func TestProvidersReachabilityFakeAPIs(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	configureTelegramProvider(t, paths)
	configureEnvProviders(t)
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	telegramSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			t.Fatalf("telegram path = %s", r.URL.Path)
		}
		writeDoctorJSON(t, w, map[string]any{"ok": true, "result": telegram.User{ID: 1, IsBot: true, Username: "onibi_test_bot"}})
	}))
	defer telegramSrv.Close()
	withTelegramProviderFactory(t, telegramSrv.URL)
	matrixSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/joined_rooms") {
			t.Fatalf("matrix path = %s", r.URL.Path)
		}
		writeDoctorJSON(t, w, map[string]any{"joined_rooms": []string{"!room:example"}})
	}))
	defer matrixSrv.Close()
	t.Setenv("ONIBI_MATRIX_HOMESERVER", matrixSrv.URL)
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth.test"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "team_id": "T1", "bot_id": "B1"})
		case strings.HasSuffix(r.URL.Path, "/apps.connections.open"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "url": "wss://socket.example"})
		default:
			t.Fatalf("slack path = %s", r.URL.Path)
		}
	}))
	defer slackSrv.Close()
	withSlackFactory(t, slackSrv.URL)
	discordSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth2/applications/@me"):
			writeDoctorJSON(t, w, discord.Application{ID: "app1", Name: "onibi"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/channels/C1"):
			writeDoctorJSON(t, w, discord.Channel{ID: "C1", GuildID: "G1"})
		default:
			t.Fatalf("discord request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer discordSrv.Close()
	withDiscordFactory(t, discordSrv.URL)
	zulipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/register":
			writeDoctorJSON(t, w, map[string]any{"queue_id": "q1", "last_event_id": 1})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/events":
			writeDoctorJSON(t, w, map[string]any{"result": "success"})
		default:
			t.Fatalf("zulip request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer zulipSrv.Close()
	t.Setenv("ONIBI_ZULIP_URL", zulipSrv.URL)
	pushoverSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages.json") {
			t.Fatalf("pushover path = %s", r.URL.Path)
		}
		writeDoctorJSON(t, w, pushover.MessageResponse{Status: 1, Request: "r1"})
	}))
	defer pushoverSrv.Close()
	withPushoverFactory(t, pushoverSrv.URL)
	ntfySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("ntfy method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ntfySrv.Close()
	t.Setenv("ONIBI_NTFY_BASE_URL", ntfySrv.URL)
	gotifySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/message") {
			t.Fatalf("gotify request = %s %s", r.Method, r.URL.Path)
		}
		writeDoctorJSON(t, w, map[string]any{"messages": []any{}})
	}))
	defer gotifySrv.Close()
	t.Setenv("ONIBI_GOTIFY_URL", gotifySrv.URL)
	withAPNsProviderFactory(t)
	report := Providers(t.Context(), Options{Paths: paths, PreferDotenv: true})
	for _, row := range report.Providers {
		if row.Reachable != ReachableYes {
			t.Fatalf("%s row = %#v", row.Name, row)
		}
	}
}

func TestProvidersLastAuditTimestamp(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	key, err := secrets.GetOrCreateStoreKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AuditAppend(t.Context(), "notify.ntfy.sent", "s1", "", 0, "approval=a1"); err != nil {
		t.Fatal(err)
	}
	if err := db.AuditAppend(t.Context(), "approval.decided", "s1", "", 42, "id=a1 verdict=approve"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	for _, name := range []string{"telegram", "ntfy"} {
		if row := providerNamed(t, report, name); row.LastAuditTimestamp == "" {
			t.Fatalf("%s row missing audit: %#v", name, row)
		}
	}
}

func TestDoctorSlackProviderFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "slack")
	t.Setenv("ONIBI_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("ONIBI_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("ONIBI_SLACK_APPROVAL_CHANNEL", "C1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth.test"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "team_id": "T1", "bot_id": "B1"})
		case strings.HasSuffix(r.URL.Path, "/apps.connections.open"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "url": "wss://socket.example"})
		case strings.HasSuffix(r.URL.Path, "/conversations.info"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "channel": map[string]any{"id": "C1", "is_member": true}})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withSlackFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "Slack live API ok") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorSlackWarnsWhenNotMember(t *testing.T) {
	paths := doctorTestPaths(t, "slack")
	t.Setenv("ONIBI_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("ONIBI_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("ONIBI_SLACK_APPROVAL_CHANNEL", "C1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth.test"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "team_id": "T1"})
		case strings.HasSuffix(r.URL.Path, "/apps.connections.open"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "url": "wss://socket.example"})
		case strings.HasSuffix(r.URL.Path, "/conversations.info"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "channel": map[string]any{"id": "C1", "is_member": false}})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withSlackFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, "not a member") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorMatrixProviderFakeAPIEncryptedWarn(t *testing.T) {
	paths := doctorTestPaths(t, "matrix")
	t.Setenv("ONIBI_MATRIX_ACCESS_TOKEN", "matrix-token")
	t.Setenv("ONIBI_MATRIX_ROOM_ID", "!room:example")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeDoctorJSON(t, w, map[string]any{"user_id": "@bot:example"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeDoctorJSON(t, w, map[string]any{"users": map[string]any{"@bot:example": 100}})
		case strings.HasSuffix(r.URL.Path, "/joined_rooms"):
			writeDoctorJSON(t, w, map[string]any{"joined_rooms": []string{"!room:example"}})
		case strings.Contains(r.URL.Path, "/state/m.room.encryption"):
			writeDoctorJSON(t, w, map[string]any{"algorithm": "m.megolm.v1.aes-sha2"})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("ONIBI_MATRIX_HOMESERVER", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, "encrypted") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorDiscordProviderFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "discord")
	t.Setenv("ONIBI_DISCORD_TOKEN", "discord-token")
	t.Setenv("ONIBI_DISCORD_CHANNEL_ID", "C1")
	t.Setenv("ONIBI_DISCORD_APPLICATION_ID", "app1")
	t.Setenv("ONIBI_DISCORD_GUILD_ID", "G1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth2/applications/@me"):
			writeDoctorJSON(t, w, discord.Application{ID: "app1", Name: "onibi"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/channels/C1"):
			writeDoctorJSON(t, w, discord.Channel{ID: "C1", GuildID: "G1"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/applications/app1/guilds/G1/commands"):
			writeDoctorJSON(t, w, []discord.ApplicationCommand{{ID: "cmd1", Name: "onibi"}})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withDiscordFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "slash command ok") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorDiscordWarnsOnSendPermission(t *testing.T) {
	paths := doctorTestPaths(t, "discord")
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_DISCORD_TOKEN", "discord-token")
	t.Setenv("ONIBI_DISCORD_CHANNEL_ID", "C1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth2/applications/@me"):
			writeDoctorJSON(t, w, discord.Application{ID: "app1", Name: "onibi"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/channels/C1"):
			writeDoctorJSON(t, w, discord.Channel{ID: "C1", GuildID: "G1"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/channels/C1/messages"):
			w.WriteHeader(http.StatusForbidden)
			writeDoctorJSON(t, w, map[string]any{"code": 50013, "message": "Missing Permissions"})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withDiscordFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, "send permission failed") || !strings.Contains(check.Detail, "50013") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorZulipProviderFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "zulip")
	t.Setenv("ONIBI_ZULIP_EMAIL", "onibi-bot@example.com")
	t.Setenv("ONIBI_ZULIP_API_KEY", "zulip-key")
	t.Setenv("ONIBI_ZULIP_STREAM", "onibi")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/register":
			writeDoctorJSON(t, w, map[string]any{"queue_id": "q1", "last_event_id": 1})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/events":
			writeDoctorJSON(t, w, map[string]any{"result": "success"})
		default:
			t.Fatalf("zulip request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("ONIBI_ZULIP_URL", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "Zulip live API ok") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorNtfyProviderPublishSubscribeFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "ntfy")
	topic := "AbcdefGhij1234567890_Z"
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_NTFY_TOPIC", topic)
	published := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ws") {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			body := <-published
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"message":`+strconvQuote(body)+`}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		published <- string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ONIBI_NTFY_BASE_URL", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "publish + WebSocket") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorGotifyProviderSendWSFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "gotify")
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_GOTIFY_URL", "placeholder")
	t.Setenv("ONIBI_GOTIFY_APP_TOKEN", "app-token")
	t.Setenv("ONIBI_GOTIFY_CLIENT_TOKEN", "client-token")
	published := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/message"):
			writeDoctorJSON(t, w, map[string]any{"messages": []any{}})
		case strings.HasPrefix(r.URL.Path, "/stream"):
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			body := <-published
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(body))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/message"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			published <- body["message"].(string)
			writeDoctorJSON(t, w, map[string]any{"id": 1})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("ONIBI_GOTIFY_URL", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "send, WebSocket") {
		t.Fatalf("check = %#v", check)
	}
}

func withSlackFactory(t *testing.T, baseURL string) {
	t.Helper()
	old := newSlackClient
	newSlackClient = func(appToken, botToken string) *slack.Client {
		c := slack.New(appToken, botToken)
		c.BaseURL = baseURL
		return c
	}
	t.Cleanup(func() { newSlackClient = old })
}

func withDiscordFactory(t *testing.T, baseURL string) {
	t.Helper()
	old := newDiscordClient
	newDiscordClient = func(token string) *discord.Client {
		c := discord.New(token)
		c.BaseURL = baseURL
		return c
	}
	t.Cleanup(func() { newDiscordClient = old })
}

func withTelegramProviderFactory(t *testing.T, baseURL string) {
	t.Helper()
	old := newTelegramProviderClient
	newTelegramProviderClient = func(token string) *telegram.Client {
		c := telegram.NewClient(token)
		c.BaseURL = baseURL
		return c
	}
	t.Cleanup(func() { newTelegramProviderClient = old })
}

func withPushoverFactory(t *testing.T, baseURL string) {
	t.Helper()
	old := newPushoverClient
	newPushoverClient = func(token, userKey string) *pushover.Client {
		c := pushover.New(token, userKey)
		c.BaseURL = baseURL
		return c
	}
	t.Cleanup(func() { newPushoverClient = old })
}

func withAPNsProviderFactory(t *testing.T) {
	t.Helper()
	old := newAPNsProviderClient
	newAPNsProviderClient = func(apns.Config) (apnsPusher, error) {
		return fakeAPNsPusher{}, nil
	}
	t.Cleanup(func() { newAPNsProviderClient = old })
}

type fakeAPNsPusher struct{}

func (fakeAPNsPusher) PushApproval(context.Context, apns.PushRequest) (apns.PushResult, error) {
	return apns.PushResult{StatusCode: http.StatusOK, APNsID: "apns-1", Sent: true}, nil
}

func configureTelegramProvider(t *testing.T, paths config.Paths) {
	t.Helper()
	key, err := secrets.GetOrCreateStoreKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(t.Context(), daemon.TelegramKVOwnerChatID, "42"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile, PreferDotenv: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(daemon.TelegramSecretBotToken, providersTestTelegramToken); err != nil {
		t.Fatal(err)
	}
}

func configureEnvProviders(t *testing.T) {
	t.Helper()
	t.Setenv("ONIBI_MATRIX_HOMESERVER", "https://matrix.example")
	t.Setenv("ONIBI_MATRIX_ACCESS_TOKEN", "matrix-token")
	t.Setenv("ONIBI_MATRIX_ROOM_ID", "!room:example")
	t.Setenv("ONIBI_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("ONIBI_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("ONIBI_DISCORD_TOKEN", "discord-token")
	t.Setenv("ONIBI_DISCORD_CHANNEL_ID", "C1")
	t.Setenv("ONIBI_ZULIP_URL", "https://zulip.example")
	t.Setenv("ONIBI_ZULIP_EMAIL", "onibi-bot@example.com")
	t.Setenv("ONIBI_ZULIP_API_KEY", "zulip-key")
	t.Setenv("ONIBI_ZULIP_STREAM", "onibi")
	t.Setenv("ONIBI_PUSHOVER_TOKEN", "push-token")
	t.Setenv("ONIBI_PUSHOVER_USER_KEY", "user-key")
	t.Setenv("ONIBI_NTFY_TOPIC", "AbcdefGhij1234567890_Z")
	t.Setenv("ONIBI_GOTIFY_URL", "https://gotify.example")
	t.Setenv("ONIBI_GOTIFY_APP_TOKEN", "app-token")
	t.Setenv("ONIBI_GOTIFY_CLIENT_TOKEN", "client-token")
	t.Setenv("ONIBI_APNS_KEY_PATH", "/tmp/AuthKey_ABC123DEFG.p8")
	t.Setenv("ONIBI_APNS_KEY_ID", "ABC123DEFG")
	t.Setenv("ONIBI_APNS_TEAM_ID", "TEAM123456")
	t.Setenv("ONIBI_APNS_TOPIC", "com.example.onibi")
	t.Setenv("ONIBI_APNS_DEVICE_TOKEN", "abc123")
}

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ONIBI_TELEGRAM_TOKEN",
		"ONIBI_MATRIX_HOMESERVER",
		"ONIBI_MATRIX_ACCESS_TOKEN",
		"ONIBI_MATRIX_ROOM_ID",
		"ONIBI_SLACK_APP_TOKEN",
		"ONIBI_SLACK_BOT_TOKEN",
		"ONIBI_SLACK_APPROVAL_CHANNEL",
		"ONIBI_SLACK_ALLOWED_CHANNELS",
		"ONIBI_DISCORD_TOKEN",
		"ONIBI_DISCORD_CHANNEL_ID",
		"ONIBI_ZULIP_URL",
		"ONIBI_ZULIP_EMAIL",
		"ONIBI_ZULIP_API_KEY",
		"ONIBI_ZULIP_STREAM",
		"ONIBI_ZULIP_TOPIC_PREFIX",
		"ONIBI_ZULIP_OWNER_EMAIL",
		"ONIBI_PUSHOVER_TOKEN",
		"ONIBI_PUSHOVER_USER_KEY",
		"ONIBI_NTFY_TOPIC",
		"ONIBI_NTFY_BASE_URL",
		"ONIBI_NTFY_TOKEN",
		"ONIBI_GOTIFY_URL",
		"ONIBI_GOTIFY_APP_TOKEN",
		"ONIBI_GOTIFY_CLIENT_TOKEN",
		"ONIBI_APNS_KEY_PATH",
		"ONIBI_APNS_KEY_ID",
		"ONIBI_APNS_TEAM_ID",
		"ONIBI_APNS_TOPIC",
		"ONIBI_APNS_DEVICE_TOKEN",
		"ONIBI_APNS_ENV",
		"ONIBI_DOCTOR_LIVE",
	} {
		t.Setenv(name, "")
	}
}

func providerNamed(t *testing.T, report ProviderReport, name string) ProviderRow {
	t.Helper()
	for _, row := range report.Providers {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("missing provider %q in %#v", name, report.Providers)
	return ProviderRow{}
}

func writeDoctorJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
