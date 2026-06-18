package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooksCodexPrintsTrustInstructions(t *testing.T) {
	_, hooksPath, _ := hooksCLIFixture(t)
	out, _ := executeRoot(t, "install-hooks", "--agent", "codex", "--color", "never")
	got := out.String()
	for _, want := range []string{
		"Codex next step: run codex, choose Review hooks, inspect onibi-notify commands, then trust if they match.",
		"Do not choose Trust all unless you have inspected every command.",
		"Continue without trusting disables these Onibi hooks for that Codex run.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if b, err := os.ReadFile(hooksPath); err != nil || strings.Contains(string(b), "onibiManaged") || strings.Contains(string(b), "onibiIntegrationVersion") {
		t.Fatalf("codex hooks contain legacy metadata: err=%v body=%s", err, b)
	}
}

func TestHooksShowCodexJSONReportsCommandsBackupAndUserHook(t *testing.T) {
	_, hooksPath, _ := hooksCLIFixture(t)
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo user-hook"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "install-hooks", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "hooks", "show", "--agent", "codex", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != hooksPath {
		t.Fatalf("config path = %q want %q", report.ConfigPath, hooksPath)
	}
	if report.Record == nil || report.Record.SHA256 == "" || report.Record.Version == "" {
		t.Fatalf("missing record: %+v", report.Record)
	}
	if report.BackupPath == "" {
		t.Fatal("backup path missing")
	}
	if _, err := os.Stat(report.BackupPath); err != nil {
		t.Fatal(err)
	}
	if len(report.Expected) != 4 {
		t.Fatalf("expected hooks = %d", len(report.Expected))
	}
	for _, ev := range []string{"SessionStart", "PreToolUse", "PostToolUse", "Stop"} {
		if !hasDrift(report.Drift, ev, "ok", "") {
			t.Fatalf("missing ok drift for %s: %+v", ev, report.Drift)
		}
	}
	if !hasDrift(report.Drift, "Stop", "extra", "user hook, not managed") {
		t.Fatalf("user hook not reported: %+v", report.Drift)
	}
}

func TestHooksShowCodexJSONAllowsMissingBackup(t *testing.T) {
	hooksCLIFixture(t)
	executeRoot(t, "install-hooks", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "hooks", "show", "--agent", "codex", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.BackupPath != "" {
		t.Fatalf("backup path = %q, want empty", report.BackupPath)
	}
	if len(report.Expected) != 4 {
		t.Fatalf("expected hooks = %d", len(report.Expected))
	}
}

func TestHooksShowCodexReportsSchemaInvalidAndTamperDrift(t *testing.T) {
	_, hooksPath, _ := hooksCLIFixture(t)
	executeRoot(t, "install-hooks", "--agent", "codex", "--color", "never")
	var cfg map[string]any
	b, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["onibiIntegrationVersion"] = "legacy"
	hooks := cfg["hooks"].(map[string]any)
	group := hooks["SessionStart"].([]any)[0].(map[string]any)
	hook := group["hooks"].([]any)[0].(map[string]any)
	hook["statusMessage"] = "tampered"
	b, err = json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "hooks", "show", "--agent", "codex", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if !hasDrift(report.Drift, "*", "schema-invalid", "unknown top-level field onibiIntegrationVersion") {
		t.Fatalf("schema drift missing: %+v", report.Drift)
	}
	if !hasDrift(report.Drift, "SessionStart", "hash-mismatch", "") {
		t.Fatalf("tamper drift missing: %+v", report.Drift)
	}
}

func hooksCLIFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	hooksPath := filepath.Join(dir, ".codex", "hooks.json")
	notify := filepath.Join(dir, "bin", "onibi-notify")
	if err := os.MkdirAll(filepath.Dir(notify), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_CODEX_HOOKS", hooksPath)
	t.Setenv("ONIBI_NOTIFY_BIN", notify)
	return dir, hooksPath, notify
}

func executeRoot(t *testing.T, args ...string) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := Root()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, out.String(), errOut.String())
	}
	return out, errOut
}

func hasDrift(rows []hookDrift, event, status, detailPart string) bool {
	for _, row := range rows {
		if row.Event != event || row.Status != status {
			continue
		}
		if detailPart == "" || strings.Contains(row.Detail, detailPart) {
			return true
		}
	}
	return false
}
