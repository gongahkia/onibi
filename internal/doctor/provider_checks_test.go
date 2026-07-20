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
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

const providersTestTelegramToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestProvidersReportAllProvidersUnconfigured(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	clearProviderEnv(t)
	report := Providers(t.Context(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	want := []string{"telegram"}
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
}

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ONIBI_TELEGRAM_TOKEN",
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
