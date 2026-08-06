package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigFailsFastForMissingCredentials(t *testing.T) {
	for _, name := range []string{"ONIBI_E2E_API_ID", "ONIBI_E2E_API_HASH", "ONIBI_E2E_SESSION_FILE", "ONIBI_E2E_BOT_USERNAME", "ONIBI_E2E_BOT_TOKEN"} {
		t.Setenv(name, "")
	}
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "ONIBI_E2E_API_ID") || !strings.Contains(err.Error(), "ONIBI_E2E_BOT_TOKEN") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadConfigAcceptsAuthorizedSessionFilePath(t *testing.T) {
	session := filepath.Join(t.TempDir(), "account.session")
	if err := os.WriteFile(session, []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_E2E_API_ID", "123")
	t.Setenv("ONIBI_E2E_API_HASH", "hash")
	t.Setenv("ONIBI_E2E_SESSION_FILE", session)
	t.Setenv("ONIBI_E2E_BOT_USERNAME", "@onibi_test_bot")
	t.Setenv("ONIBI_E2E_BOT_TOKEN", "token")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.appID != 123 || cfg.botUsername != "onibi_test_bot" || cfg.sessionFile != session {
		t.Fatalf("cfg=%#v", cfg)
	}
}
