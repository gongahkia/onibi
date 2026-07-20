package doctor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/secrets"
	signalapi "github.com/gongahkia/onibi/internal/signal"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

const providersTestTelegramToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestProvidersReportAllProvidersUnconfigured(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	want := []string{"telegram", "matrix", "slack", "discord", "zulip", "irc", "signal"}
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
		"irc":      "ONIBI_IRC_NICK",
		"signal":   "ONIBI_SIGNAL_RPC_URL",
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
	t.Setenv("ONIBI_SIGNAL_RPC_URL", "http://signal.invalid")
	withSignalFactory(t)
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
	withIRCFactory(t)
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
	if err := db.AuditAppend(t.Context(), "approval.decided", "s1", "", 42, "id=a1 verdict=approve"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	for _, name := range []string{"telegram"} {
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

func TestDoctorIRCProviderFakeConn(t *testing.T) {
	paths := doctorTestPaths(t, "irc")
	t.Setenv("ONIBI_IRC_NICK", "onibi")
	t.Setenv("ONIBI_IRC_USERNAME", "onibi")
	t.Setenv("ONIBI_IRC_PASSWORD", "irc-pass")
	t.Setenv("ONIBI_IRC_OWNER_NICK", "owner")
	withIRCFactory(t)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "IRC live API ok") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorSignalProviderFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "signal")
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_SIGNAL_RPC_URL", "http://signal.invalid")
	t.Setenv("ONIBI_SIGNAL_ACCOUNT", "+15550001")
	t.Setenv("ONIBI_SIGNAL_RECIPIENT", "+15550002")
	withSignalFactory(t)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "Signal daemon check ok") {
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

func withIRCFactory(t *testing.T) {
	t.Helper()
	old := newIRCClient
	newIRCClient = func(addr, nick, username, password string) *irc.Client {
		clientConn, serverConn := net.Pipe()
		c := irc.New("pipe", nick, username, password)
		c.Plaintext = true
		c.Dial = func(context.Context, string, string) (net.Conn, error) {
			go serveIRCSASL(t, serverConn)
			return clientConn, nil
		}
		return c
	}
	t.Cleanup(func() { newIRCClient = old })
}

func withSignalFactory(t *testing.T) {
	t.Helper()
	old := newSignalClient
	newSignalClient = func(baseURL, account string) *signalapi.Client {
		c := signalapi.New(baseURL, account)
		c.HTTP = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}).Client()
		return c
	}
	t.Cleanup(func() { newSignalClient = old })
}

func serveIRCSASL(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()
	r := bufio.NewReader(conn)
	for _, want := range []string{"CAP REQ :sasl", "NICK onibi", "USER onibi 0 * :Onibi"} {
		got, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimRight(got, "\r\n") != want {
			t.Errorf("irc line = %q want %q", strings.TrimRight(got, "\r\n"), want)
			return
		}
	}
	_, _ = conn.Write([]byte(":irc.test CAP onibi ACK :sasl\r\n"))
	if got, err := r.ReadString('\n'); err != nil || strings.TrimRight(got, "\r\n") != "AUTHENTICATE PLAIN" {
		return
	}
	_, _ = conn.Write([]byte("AUTHENTICATE +\r\n"))
	if _, err := r.ReadString('\n'); err != nil {
		return
	}
	_, _ = conn.Write([]byte(":irc.test 903 onibi :SASL authentication successful\r\n"))
	if got, err := r.ReadString('\n'); err != nil || strings.TrimRight(got, "\r\n") != "CAP END" {
		return
	}
	_, _ = conn.Write([]byte(":irc.test 001 onibi :Welcome\r\n"))
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (f roundTripFunc) Client() *http.Client {
	return &http.Client{Transport: f}
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
	t.Setenv("ONIBI_IRC_NICK", "onibi")
	t.Setenv("ONIBI_IRC_USERNAME", "onibi")
	t.Setenv("ONIBI_IRC_PASSWORD", "irc-pass")
	t.Setenv("ONIBI_IRC_OWNER_NICK", "owner")
	t.Setenv("ONIBI_SIGNAL_RPC_URL", "http://signal.invalid")
	t.Setenv("ONIBI_SIGNAL_ACCOUNT", "+15550001")
	t.Setenv("ONIBI_SIGNAL_RECIPIENT", "+15550002")
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
		"ONIBI_IRC_ADDR",
		"ONIBI_IRC_NICK",
		"ONIBI_IRC_USERNAME",
		"ONIBI_IRC_PASSWORD",
		"ONIBI_IRC_OWNER_NICK",
		"ONIBI_IRC_PLAINTEXT",
		"ONIBI_SIGNAL_RPC_URL",
		"ONIBI_SIGNAL_ACCOUNT",
		"ONIBI_SIGNAL_RECIPIENT",
		"ONIBI_SIGNAL_RECIPIENTS",
		"ONIBI_SIGNAL_GROUP_ID",
		"ONIBI_SIGNAL_OWNER",
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
