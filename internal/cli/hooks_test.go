package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
)

func TestInstallHooksCodexPrintsTrustInstructions(t *testing.T) {
	_, hooksPath, _ := hooksCLIFixture(t)
	out, _ := executeRoot(t, "agent", "install", "--agent", "codex", "--color", "never")
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

func TestInstallHooksAutoDetectDryRun(t *testing.T) {
	home, hooksPath, _ := hooksCLIFixture(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "agent", "install", "--dry-run", "--color", "never")
	got := out.String()
	for _, want := range []string{
		"[PLAN] Install agent claude hook:",
		"[PLAN] Install agent codex hook:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry-run missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote claude settings: %v", err)
	}
	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote codex hooks: %v", err)
	}
}

func TestInstallHooksAutoDetectAllInstallsPresent(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "agent", "install", "--all", "--color", "never")
	got := out.String()
	for _, want := range []string{"Installed claude hooks"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallHooksAutoDetectPrompts(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRootInput(t, "n\n", "agent", "install", "--color", "never")
	if !strings.Contains(out.String(), "Install agent claude hook? [Y/n]") {
		t.Fatalf("missing prompt:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("prompt no wrote claude settings: %v", err)
	}
}

func TestShellHookFlagsRejected(t *testing.T) {
	hooksCLIFixture(t)
	for _, args := range [][]string{
		{"agent", "install", "--shell", "zsh", "--color", "never"},
		{"agent", "inspect", "--shell", "zsh", "--color", "never"},
	} {
		_, _, err := executeRootAllowError(t, args...)
		if err == nil || !strings.Contains(err.Error(), "unknown flag: --shell") {
			t.Fatalf("args=%v err=%v", args, err)
		}
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
	executeRoot(t, "agent", "install", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "codex", "--json", "--color", "never")
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
	executeRoot(t, "agent", "install", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "codex", "--json", "--color", "never")
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
	executeRoot(t, "agent", "install", "--agent", "claude", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "claude", "--json", "--color", "never")
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
	executeRoot(t, "agent", "install", "--agent", "goose", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "goose", "--json", "--color", "never")
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

func TestHooksShowGeminiJSONReportsCommandsBackupAndDisable(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	settingsPath := filepath.Join(home, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_GEMINI_SETTINGS", settingsPath)
	executeRoot(t, "agent", "install", "--agent", "gemini", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "gemini", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != settingsPath || report.BackupPath == "" {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Expected) != 7 || len(report.Observed) != 7 {
		t.Fatalf("hooks=%+v", report)
	}
	for _, event := range []string{"SessionStart", "BeforeAgent", "BeforeTool", "AfterTool", "Notification", "AfterAgent", "SessionEnd"} {
		if !hasDrift(report.Drift, event, "ok", "") {
			t.Fatalf("missing %s drift: %+v", event, report.Drift)
		}
	}
	trust := strings.Join(report.TrustInstructions, "\n")
	if !strings.Contains(trust, "inspect the configured hooks") || !strings.Contains(trust, "agent inspect") {
		t.Fatalf("trust=%q", trust)
	}
}

func TestHooksShowCopilotJSONReportsCommandsBackupTrustAndDisabled(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	hooksPath := filepath.Join(home, ".copilot", "hooks", "onibi.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"version":1,"disableAllHooks":true,"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "agent", "install", "--agent", "copilot", "--color", "never")
	var cfg map[string]any
	b, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["disableAllHooks"] = true
	b, err = json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "copilot", "--json", "--color", "never")
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
	if !strings.Contains(report.Message, "disableAllHooks=true") {
		t.Fatalf("message missing disableAllHooks: %+v", report)
	}
	if len(report.Expected) != 9 {
		t.Fatalf("expected hooks = %d", len(report.Expected))
	}
	for _, ev := range []string{"sessionStart", "userPromptSubmitted", "preToolUse", "postToolUse", "postToolUseFailure", "notification", "agentStop", "sessionEnd", "errorOccurred"} {
		if !hasDrift(report.Drift, ev, "ok", "") {
			t.Fatalf("missing ok drift for %s: %+v", ev, report.Drift)
		}
	}
	trust := strings.Join(report.TrustInstructions, "\n")
	for _, want := range []string{"restart Copilot CLI", "COPILOT_HOME", "disableAllHooks"} {
		if !strings.Contains(trust, want) {
			t.Fatalf("trust instructions missing %q: %+v", want, report.TrustInstructions)
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
	executeRoot(t, "agent", "install", "--agent", "amp", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "amp", "--json", "--color", "never")
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
	if len(report.Expected) != 5 || len(report.Observed) != 5 {
		t.Fatalf("hooks expected=%+v observed=%+v", report.Expected, report.Observed)
	}
	for _, event := range []string{"session.start", "agent.start", "tool.call", "tool.result", "agent.end"} {
		if !hasDrift(report.Drift, event, "ok", "") {
			t.Fatalf("missing ok drift for %s: %+v", event, report.Drift)
		}
	}
	trust := strings.Join(report.TrustInstructions, "\n")
	for _, want := range []string{"plugins: reload", "plugins: list"} {
		if !strings.Contains(trust, want) {
			t.Fatalf("trust instructions missing %q: %+v", want, report.TrustInstructions)
		}
	}
}

func TestHooksShowOpenCodeJSONReportsPluginStateAndReload(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	pluginPath := filepath.Join(home, ".config", "opencode", "plugins", "onibi.js")
	t.Setenv("ONIBI_OPENCODE_PLUGIN", pluginPath)
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("old OpenCode plugin"), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "agent", "install", "--agent", "opencode", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "opencode", "--json", "--color", "never")
	var report hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.ConfigPath != pluginPath || report.BackupPath == "" {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Expected) != 4 || len(report.Observed) != 4 {
		t.Fatalf("plugin hooks=%+v", report)
	}
	for _, event := range []string{"event", "tool.execute.before", "tool.execute.after", "session.idle"} {
		if !hasDrift(report.Drift, event, "ok", "") {
			t.Fatalf("missing %s drift: %+v", event, report.Drift)
		}
	}
	trust := strings.Join(report.TrustInstructions, "\n")
	for _, want := range []string{"restart OpenCode", "ONIBI_OPENCODE_SCOPE=project", "ONIBI_OPENCODE_PLUGIN"} {
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
	executeRoot(t, "agent", "install", "--agent", "pi", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "pi", "--json", "--color", "never")
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
	executeRoot(t, "agent", "install", "--agent", "codex", "--color", "never")
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
	out, _ := executeRoot(t, "agent", "inspect", "--agent", "codex", "--json", "--color", "never")
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

func TestHooksMatrixIncludesAgents(t *testing.T) {
	hooksCLIFixture(t)
	out, _ := executeRoot(t, "agent", "matrix", "--json", "--color", "never")
	var rows []hooksMatrixRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if row, ok := matrixRow(rows, "goose"); !ok || row.Support != "blocking" {
		t.Fatalf("goose row = %+v ok=%v", row, ok)
	}
	if row, ok := matrixRow(rows, "codex"); !ok || row.Support != "blocking" || row.NextAction == "" {
		t.Fatalf("codex row = %+v ok=%v", row, ok)
	}
	if _, ok := matrixRow(rows, "shell:zsh"); ok {
		t.Fatalf("unexpected shell row: %+v", rows)
	}
}

func TestHooksMatrixReportsInstalledCodexDriftAndManualStep(t *testing.T) {
	hooksCLIFixture(t)
	executeRoot(t, "agent", "install", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "agent", "matrix", "--json", "--color", "never")
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

func TestHooksShowAllComparesBundledAndObservedVersions(t *testing.T) {
	hooksCLIFixture(t)
	executeRoot(t, "agent", "install", "--agent", "codex", "--color", "never")
	out, _ := executeRoot(t, "agent", "inspect", "--all", "--json", "--color", "never")
	var reports []hooksShowReport
	if err := json.Unmarshal(out.Bytes(), &reports); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	report, ok := hooksReport(reports, "codex")
	if !ok {
		t.Fatalf("missing codex report: %+v", reports)
	}
	if report.ObservedVersion != common.IntegrationVersion || report.BundledVersion != common.IntegrationVersion || report.VersionStatus != "ok" {
		t.Fatalf("version compare = %+v", report)
	}
	if _, ok := hooksReport(reports, "shell:zsh"); ok {
		t.Fatalf("unexpected shell report: %+v", reports)
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
	return executeRootInput(t, "", args...)
}

func executeRootInput(t *testing.T, input string, args ...string) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := Root()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetIn(strings.NewReader(input))
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

func hooksReport(reports []hooksShowReport, agent string) (hooksShowReport, bool) {
	for _, report := range reports {
		if report.Agent == agent {
			return report, true
		}
	}
	return hooksShowReport{}, false
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
