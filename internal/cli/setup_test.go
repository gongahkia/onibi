package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

func TestSetupCompleteAbortsWhenNotifyMissingAndNonInteractive(t *testing.T) {
	paths, db := setupCompleteFixture(t)
	cmd, _, errOut := setupCompleteCmd("n\n\n")
	oldLocate := locateNotifyBinary
	oldInput := inputIsTerminal
	oldDoctor := doctorRun
	locateNotifyBinary = func() (string, error) { return "", errors.New("missing notify") }
	inputIsTerminal = func(any) bool { return false }
	doctorRun = func(context.Context, doctor.Options) doctor.Report {
		t.Fatal("doctor should not run after hook abort")
		return doctor.Report{}
	}
	t.Cleanup(func() {
		locateNotifyBinary = oldLocate
		inputIsTerminal = oldInput
		doctorRun = oldDoctor
	})
	err := runSetupComplete(cmd, paths, db)
	if err == nil || !strings.Contains(err.Error(), "hooks step aborted") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(errOut.String(), "onibi-notify not found. Remediation:") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestSetupCompleteContinuesWhenUserConfirmsMissingNotify(t *testing.T) {
	paths, db := setupCompleteFixture(t)
	cmd, out, errOut := setupCompleteCmd("n\n\ny\n")
	oldLocate := locateNotifyBinary
	oldInput := inputIsTerminal
	oldDoctor := doctorRun
	locateNotifyBinary = func() (string, error) { return "", errors.New("missing notify") }
	inputIsTerminal = func(any) bool { return true }
	doctorCalled := false
	doctorRun = func(context.Context, doctor.Options) doctor.Report {
		doctorCalled = true
		return doctor.Report{Checks: []doctor.Check{{Name: "ok", Status: doctor.Pass, Detail: "ok"}}}
	}
	t.Cleanup(func() {
		locateNotifyBinary = oldLocate
		inputIsTerminal = oldInput
		doctorRun = oldDoctor
	})
	if err := runSetupComplete(cmd, paths, db); err != nil {
		t.Fatal(err)
	}
	if !doctorCalled {
		t.Fatal("doctor did not run")
	}
	if !strings.Contains(errOut.String(), "onibi-notify not found. Remediation:") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Doctor summary:") {
		t.Fatalf("stdout = %q", out.String())
	}
	if !strings.Contains(out.String(), "onibi demo --approval") {
		t.Fatalf("missing next action: %q", out.String())
	}
}

func TestSetupEncryptedPrintsURLBeforeQR(t *testing.T) {
	paths, _ := setupCompleteFixture(t)
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile, PreferDotenv: true})
	if err != nil {
		t.Fatal(err)
	}
	cmd, out, _ := setupCompleteCmd("")
	if err := runSetupEncrypted(cmd, paths, sec, "on", "https://example.com/mini"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	urlIdx := strings.Index(got, "Open this URL in Telegram")
	qrIdx := strings.Index(got, "Or scan this QR")
	if urlIdx < 0 || qrIdx < 0 || urlIdx > qrIdx {
		t.Fatalf("output order wrong:\n%s", got)
	}
	if !strings.Contains(got, "Plaintext entry will refuse in encrypted mode") {
		t.Fatalf("missing plaintext refusal note:\n%s", got)
	}
}

func setupCompleteFixture(t *testing.T) (config.Paths, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{
		StateDir: filepath.Join(dir, "state"),
		Socket:   filepath.Join(dir, "state", "onibi.sock"),
		DBFile:   filepath.Join(dir, "state", "onibi.sqlite"),
		EnvFile:  filepath.Join(dir, "state", ".env"),
		LogDir:   filepath.Join(dir, "state", "logs"),
		Config:   filepath.Join(dir, "state", "config.yaml"),
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return paths, db
}

func setupCompleteCmd(input string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	return cmd, out, errOut
}
