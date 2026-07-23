package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

const cliTelegramTestToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestTelegramSetupStatusDisableCLI(t *testing.T) {
	paths := withDefaultState(t)
	withDotenvSecretStore(t)
	withFakeTelegramClient(t)

	out, _ := executeRoot(t, "telegram", "setup", "--token", cliTelegramTestToken, "--color", "never")
	if !strings.Contains(out.String(), "Stored Telegram bot @onibi_test_bot") || !strings.Contains(out.String(), "Pair: send /start") {
		t.Fatalf("setup output:\n%s", out.String())
	}
	out, _ = executeRoot(t, "telegram", "status", "--color", "never")
	if !strings.Contains(out.String(), "token") || !strings.Contains(out.String(), "true") {
		t.Fatalf("status output:\n%s", out.String())
	}
	out, _ = executeRoot(t, "telegram", "status", "--json", "--check", "--color", "never")
	var status telegramStatusReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("status json: %v\n%s", err, out.String())
	}
	if !status.Token || status.TokenValid == nil || !*status.TokenValid || status.BotUsername != "onibi_test_bot" {
		t.Fatalf("status = %+v", status)
	}
	if status.Capability.Version != telegramCapabilityVersion || !status.Capability.OwnerOnly || !status.Capability.Standalone || status.Capability.PWARequired || status.Capability.LiveTerminal || status.Capability.EndToEndEncrypted || !status.Capability.BoundedRedactedOutput || !slices.Equal(status.Capability.ApprovalVerdicts, []string{"approve", "deny", "edit"}) {
		t.Fatalf("capability = %+v", status.Capability)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(context.Background(), daemon.TelegramKVOwnerChatID, "42"); err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(context.Background(), daemon.TelegramKVOwnerUserID, "7"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "telegram", "disable", "--color", "never")

	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Get(daemon.TelegramSecretBotToken); err != nil || ok {
		t.Fatalf("token after disable ok=%v err=%v", ok, err)
	}
	db, err = store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, key := range []string{daemon.TelegramKVOwnerChatID, daemon.TelegramKVOwnerUserID, daemon.TelegramKVPairCode} {
		if _, ok, err := db.KVGetString(context.Background(), key); err != nil || ok {
			t.Fatalf("%s after disable ok=%v err=%v", key, ok, err)
		}
	}
}

func TestTelegramSetupNoCheckStoresLocalToken(t *testing.T) {
	paths := withDefaultState(t)
	withDotenvSecretStore(t)
	old := newTelegramClient
	newTelegramClient = func(string) *telegram.Client {
		t.Fatal("no-check must not call Telegram")
		return nil
	}
	t.Cleanup(func() { newTelegramClient = old })

	out, _ := executeRoot(t, "telegram", "setup", "--no-check", "--token", cliTelegramTestToken, "--color", "never")
	for _, want := range []string{"live check skipped", "Pair: send /start", "Check: onibi telegram status --check"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("setup output missing %q:\n%s", want, out.String())
		}
	}
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := st.Get(daemon.TelegramSecretBotToken); err != nil || !ok || got != cliTelegramTestToken {
		t.Fatalf("stored token got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestTelegramStatusUsesEnvToken(t *testing.T) {
	withDefaultState(t)
	withDotenvSecretStore(t)
	withFakeTelegramClient(t)
	t.Setenv(telegramTokenEnv, cliTelegramTestToken)

	out, _ := executeRoot(t, "telegram", "status", "--json", "--check", "--color", "never")
	var status telegramStatusReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("status json: %v\n%s", err, out.String())
	}
	if !status.Token || status.SecretBackend != "env" || status.TokenValid == nil || !*status.TokenValid {
		t.Fatalf("status = %+v", status)
	}
}

func TestTelegramOwnerBindingRejectsLegacyChatOnlyState(t *testing.T) {
	paths := withDefaultState(t)
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.KVSetString(t.Context(), daemon.TelegramKVOwnerChatID, "42"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := telegramOwnerBinding(t.Context(), db); err == nil || !strings.Contains(err.Error(), "owner binding is incomplete") {
		t.Fatalf("legacy binding error = %v", err)
	}
}

func TestTelegramRemainsExplicitOptInDefaultTransport(t *testing.T) {
	if got := config.Default().Transport.Mode; got != "lan" {
		t.Fatalf("default transport = %q", got)
	}
}

func TestTelegramIsATopLevelCommand(t *testing.T) {
	out, _ := executeRoot(t, "telegram", "--help", "--color", "never")
	if !strings.Contains(out.String(), "Telegram") {
		t.Fatalf("output = %q", out.String())
	}
}

func withDotenvSecretStore(t *testing.T) {
	t.Helper()
	old := openSecretStore
	openSecretStore = func(opts secrets.Options) (*secrets.Store, error) {
		opts.PreferDotenv = true
		return secrets.Open(opts)
	}
	t.Cleanup(func() { openSecretStore = old })
}

func withFakeTelegramClient(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			t.Fatalf("unexpected telegram path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": telegram.User{
				ID:       7,
				IsBot:    true,
				Username: "onibi_test_bot",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(srv.Close)
	old := newTelegramClient
	newTelegramClient = func(token string) *telegram.Client {
		c := telegram.NewClient(token)
		c.BaseURL = srv.URL
		c.HTTP = srv.Client()
		return c
	}
	t.Cleanup(func() { newTelegramClient = old })
}
