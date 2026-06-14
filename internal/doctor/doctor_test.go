package doctor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/store"
)

type okRunner struct{}

func (okRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "systemctl" && len(args) >= 2 && args[1] == "is-active" {
		return []byte("active\n"), nil
	}
	return []byte("ok\n"), nil
}

func doctorPaths(t *testing.T) config.Paths {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "onibi-doctor-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return config.Paths{
		StateDir: filepath.Join(dir, "state"),
		Socket:   filepath.Join(dir, "state", "onibi.sock"),
		DBFile:   filepath.Join(dir, "state", "onibi.sqlite"),
		EnvFile:  filepath.Join(dir, "state", ".env"),
		LogDir:   filepath.Join(dir, "state", "logs"),
	}
}

func TestDoctorOfflinePassesRequiredChecks(t *testing.T) {
	paths := doctorPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	owner := &auth.Owner{}
	if err := auth.SetOwner(context.Background(), db, owner, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(context.Background(), auth.KVKeyBotID, "123"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile, PreferDotenv: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(secrets.KeyBotToken, "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(paths.Socket)
	})
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	unit := filepath.Join(home, ".config", "systemd", "user", service.UnitName)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &service.Manager{
		Paths:      paths,
		Executable: "/usr/local/bin/onibi",
		Runner:     okRunner{},
		GOOS:       "linux",
		Home:       home,
		UID:        1000,
	}
	report := Run(context.Background(), Options{
		Paths:        paths,
		Offline:      true,
		Service:      m,
		PreferDotenv: true,
	})
	if report.Failed() {
		t.Fatalf("unexpected fail: %#v", report.Checks)
	}
}

func TestDoctorFailsMissingOwner(t *testing.T) {
	paths := doctorPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile, PreferDotenv: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(secrets.KeyBotToken, "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), Options{Paths: paths, Offline: true, PreferDotenv: true, Mode: "installed"})
	if !report.Failed() {
		t.Fatalf("expected failure: %#v", report.Checks)
	}
}

func TestDoctorPreflightWarnsMissingOwner(t *testing.T) {
	paths := doctorPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	report := Run(context.Background(), Options{Paths: paths, Offline: true, PreferDotenv: true, Mode: "preflight"})
	if report.Failed() {
		t.Fatalf("preflight should warn, not fail: %#v", report.Checks)
	}
	found := false
	for _, c := range report.Checks {
		if c.Name == "owner chat_id" && strings.Contains(c.Next, "setup --complete") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing next action: %#v", report.Checks)
	}
}

func TestDoctorWarnsTelegram2FATimeout(t *testing.T) {
	paths := doctorPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.KVSetString(context.Background(), "tg_2fa_ack", "timeout"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	report := Run(context.Background(), Options{Paths: paths, Offline: true, PreferDotenv: true, Mode: "preflight"})
	found := false
	for _, c := range report.Checks {
		if c.Name != "telegram 2fa ack" {
			continue
		}
		found = true
		if c.Status != Warn || !strings.Contains(c.Detail, "timed out during setup") {
			t.Fatalf("check = %#v", c)
		}
	}
	if !found {
		t.Fatalf("missing telegram 2fa ack: %#v", report.Checks)
	}
}

func TestFixAdoptsMissingHookHash(t *testing.T) {
	paths := doctorPaths(t)
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	notify := filepath.Join(t.TempDir(), "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_CODEX_HOOKS", filepath.Join(t.TempDir(), "codex-hooks.json"))
	a, _ := adapters.Get("codex")
	if err := a.Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(context.Background(), `DELETE FROM hooks WHERE agent = 'codex'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	fixes := Fix(context.Background(), Options{Paths: paths, Offline: true, PreferDotenv: true})
	if fixes.Failed() {
		t.Fatalf("fix failed: %#v", fixes.Errors)
	}
	db, err = store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	info := a.Status(context.Background(), db)
	if !info.HashRecorded || info.Adoptable || info.Tampered {
		t.Fatalf("hook not adopted: %+v actions=%v", info, fixes.Actions)
	}
}
