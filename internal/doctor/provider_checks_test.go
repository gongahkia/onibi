package doctor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
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
	want := []string{"telegram", "matrix", "slack"}
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
