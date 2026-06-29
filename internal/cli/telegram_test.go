package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(context.Background(), daemon.TelegramKVOwnerChatID, "42"); err != nil {
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
	for _, key := range []string{daemon.TelegramKVOwnerChatID, daemon.TelegramKVPairCode} {
		if _, ok, err := db.KVGetString(context.Background(), key); err != nil || ok {
			t.Fatalf("%s after disable ok=%v err=%v", key, ok, err)
		}
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
