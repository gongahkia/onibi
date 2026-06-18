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

func TestHooksShowClaudeJSONReportsCommandsBackupAndTrust(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo user-hook"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "install-hooks", "--agent", "claude", "--color", "never")
	out, _ := executeRoot(t, "hooks", "show", "--agent", "claude", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != settingsPath {
		t.Fatalf("config path = %q want %q", report.ConfigPath, settingsPath)
	}
	if report.BackupPath == "" {
		t.Fatal("backup path missing")
	}
	if len(report.Expected) != 7 {
		t.Fatalf("expected hooks = %d", len(report.Expected))
	}
	if len(report.TrustInstructions) == 0 || !strings.Contains(strings.Join(report.TrustInstructions, "\n"), "/hooks") {
		t.Fatalf("trust instructions missing /hooks: %+v", report.TrustInstructions)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop", "SessionEnd"} {
		if !hasDrift(report.Drift, ev, "ok", "") {
			t.Fatalf("missing ok drift for %s: %+v", ev, report.Drift)
		}
	}
	if !hasDrift(report.Drift, "Stop", "extra", "user hook, not managed") {
		t.Fatalf("user hook not reported: %+v", report.Drift)
	}
}

func TestHooksShowGooseJSONReportsCommandsAndBackup(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	hooksPath := filepath.Join(home, ".agents", "plugins", "onibi", "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "install-hooks", "--agent", "goose", "--color", "never")
	out, _ := executeRoot(t, "hooks", "show", "--agent", "goose", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != hooksPath {
		t.Fatalf("config path = %q want %q", report.ConfigPath, hooksPath)
	}
	if report.BackupPath == "" {
		t.Fatal("backup path missing")
	}
	if len(report.Expected) != 11 {
		t.Fatalf("expected hooks = %d", len(report.Expected))
	}
	for _, ev := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "BeforeReadFile", "AfterFileEdit", "BeforeShellExecution", "AfterShellExecution", "Stop"} {
		if !hasDrift(report.Drift, ev, "ok", "") {
			t.Fatalf("missing ok drift for %s: %+v", ev, report.Drift)
		}
	}
}

func TestHooksShowAmpJSONReportsPluginPathAndReload(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	pluginPath := filepath.Join(home, ".config", "amp", "plugins", "onibi.ts")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("old amp plugin"), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "install-hooks", "--agent", "amp", "--color", "never")
	out, _ := executeRoot(t, "hooks", "show", "--agent", "amp", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != pluginPath {
		t.Fatalf("config path = %q want %q", report.ConfigPath, pluginPath)
	}
	if report.BackupPath == "" {
		t.Fatal("backup path missing")
	}
	trust := strings.Join(report.TrustInstructions, "\n")
	for _, want := range []string{"plugins: reload", "plugins: list"} {
		if !strings.Contains(trust, want) {
			t.Fatalf("trust instructions missing %q: %+v", want, report.TrustInstructions)
		}
	}
}

func TestHooksShowPiJSONReportsExtensionPathAndReload(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	extensionPath := filepath.Join(home, ".pi", "agent", "extensions", "onibi.ts")
	if err := os.MkdirAll(filepath.Dir(extensionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extensionPath, []byte("old pi extension"), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "install-hooks", "--agent", "pi", "--color", "never")
	out, _ := executeRoot(t, "hooks", "show", "--agent", "pi", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != extensionPath {
		t.Fatalf("config path = %q want %q", report.ConfigPath, extensionPath)
	}
	if report.BackupPath == "" {
		t.Fatal("backup path missing")
	}
	trust := strings.Join(report.TrustInstructions, "\n")
	for _, want := range []string{"/reload", "ONIBI_PI_SCOPE=project", "ONIBI_PI_EXTENSION"} {
		if !strings.Contains(trust, want) {
			t.Fatalf("trust instructions missing %q: %+v", want, report.TrustInstructions)
		}
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

func TestHooksMatrixIncludesAgentsAndShells(t *testing.T) {
	hooksCLIFixture(t)
	out, _ := executeRoot(t, "hooks", "matrix", "--json", "--color", "never")
	var rows []hooksMatrixRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if row, ok := matrixRow(rows, "goose"); !ok || row.Support != "event-bridge" {
		t.Fatalf("goose row = %+v ok=%v", row, ok)
	}
	if row, ok := matrixRow(rows, "codex"); !ok || row.Support != "blocking" || row.NextAction == "" {
		t.Fatalf("codex row = %+v ok=%v", row, ok)
	}
	if row, ok := matrixRow(rows, "shell:zsh"); !ok || row.Support != "event-bridge" || row.ConfigSchemaStatus != "n/a" {
		t.Fatalf("shell row = %+v ok=%v", row, ok)
	}
}

func TestHooksMatrixReportsInstalledCodexDriftAndManualStep(t *testing.T) {
	hooksCLIFixture(t)
	executeRoot(t, "install-hooks", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "hooks", "matrix", "--json", "--color", "never")
	var rows []hooksMatrixRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	row, ok := matrixRow(rows, "codex")
	if !ok {
		t.Fatalf("missing codex row: %+v", rows)
	}
	if row.ConfigSchemaStatus != "ok" || row.HashStatus != "ok" || row.Drift != "ok" {
		t.Fatalf("codex matrix = %+v", row)
	}
	if !strings.Contains(row.TrustedManualStep, "Review hooks") || !strings.Contains(row.NextAction, "Review hooks") {
		t.Fatalf("manual step missing: %+v", row)
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

func matrixRow(rows []hooksMatrixRow, provider string) (hooksMatrixRow, bool) {
	for _, row := range rows {
		if row.Provider == provider {
			return row, true
		}
	}
	return hooksMatrixRow{}, false
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
