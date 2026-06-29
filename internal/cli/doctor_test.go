package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

func TestDoctorJSONIsValid(t *testing.T) {
	withDotenvDoctor(t)
	out, _, err := executeRootAllowError(t, "doctor", "--offline", "--mode", "preflight", "--json", "--color", "never")
	if err != nil && !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("execute doctor: %v", err)
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(report.Checks) == 0 {
		t.Fatal("missing checks")
	}
}

func TestDoctorExplainPrintsRepairPlan(t *testing.T) {
	withDotenvDoctor(t)
	out, _, err := executeRootAllowError(t, "doctor", "--offline", "--mode", "preflight", "--explain", "--color", "never")
	if err != nil && !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("execute doctor: %v", err)
	}
	got := out.String()
	for _, want := range []string{"next=", "impact:", "safe fix:", "manual fix:", "files:", "retry:", "blocks:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorTransportOverrideReportsProvider(t *testing.T) {
	withDotenvDoctor(t)
	out, _, err := executeRootAllowError(t, "doctor", "--offline", "--transport", "matrix", "--json", "--color", "never")
	if err != nil && !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("execute doctor: %v", err)
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	for _, check := range report.Checks {
		if check.Name == "transport provider" {
			if !strings.Contains(check.Detail, "ONIBI_MATRIX_HOMESERVER") {
				t.Fatalf("provider detail = %q", check.Detail)
			}
			return
		}
	}
	t.Fatalf("missing transport provider check: %#v", report.Checks)
}

func TestDoctorFixDoesNotInstallHooksOnFreshState(t *testing.T) {
	paths := withDefaultState(t)
	notify := filepath.Join(paths.StateDir, "onibi-notify")
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_NOTIFY_BIN", notify)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_CODEX_HOOKS", filepath.Join(home, ".codex", "hooks.json"))
	t.Setenv("ONIBI_COPILOT_HOOK", filepath.Join(home, ".copilot", "hooks", "onibi.json"))
	t.Setenv("ONIBI_GEMINI_SETTINGS", filepath.Join(home, ".gemini", "settings.json"))
	t.Setenv("ONIBI_GOOSE_HOOKS", filepath.Join(home, ".agents", "plugins", "onibi", "hooks", "hooks.json"))
	t.Setenv("ONIBI_AMP_PLUGIN", filepath.Join(home, ".config", "amp", "plugins", "onibi.ts"))
	t.Setenv("ONIBI_OPENCODE_PLUGIN", filepath.Join(home, ".opencode", "plugins", "onibi.js"))
	t.Setenv("ONIBI_PI_EXTENSION", filepath.Join(home, ".pi", "agent", "extensions", "onibi.ts"))
	out, _, err := executeRootAllowError(t, "doctor", "--offline", "--fix", "--color", "never")
	if err != nil && !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("execute doctor --fix: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "reinstalled ") {
		t.Fatalf("fresh doctor --fix installed hooks:\n%s", out.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".codex", "hooks.json"),
		filepath.Join(home, ".copilot", "hooks", "onibi.json"),
		filepath.Join(home, ".gemini", "settings.json"),
		filepath.Join(home, ".agents", "plugins", "onibi", "hooks", "hooks.json"),
		filepath.Join(home, ".config", "amp", "plugins", "onibi.ts"),
		filepath.Join(home, ".opencode", "plugins", "onibi.js"),
		filepath.Join(home, ".pi", "agent", "extensions", "onibi.ts"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".config", "fish", "conf.d", "onibi.fish"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("fresh doctor --fix created %s (err=%v)", path, err)
		}
	}
}

func TestDoctorReleaseModeIncludesUpdateTelegramAndAfterUpgrade(t *testing.T) {
	withDotenvDoctor(t)
	withFakeUpdateCheck(t, func() updatecheck.Result {
		return updatecheck.Result{Status: updatecheck.StatusCurrent, Source: updatecheck.SourceGitHub, Detail: "current"}
	})
	out, _, err := executeRootAllowError(t, "doctor", "--mode", "release", "--json", "--color", "never")
	if err != nil && !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("execute release doctor: %v\n%s", err, out.String())
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	for _, want := range []string{"update check", "telegram optional", "after-upgrade hooks"} {
		if !hasDoctorCheck(report, want) {
			t.Fatalf("missing %q in %#v", want, report.Checks)
		}
	}
}

func TestDoctorReleaseModeFailsOnBlockedUpdate(t *testing.T) {
	withDotenvDoctor(t)
	withFakeUpdateCheck(t, func() updatecheck.Result {
		return updatecheck.Result{Status: updatecheck.StatusUnavailable, Source: updatecheck.SourceNone, Detail: "cannot verify"}
	})
	out, _, err := executeRootAllowError(t, "doctor", "--release", "--json", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "doctor failed") {
		t.Fatalf("expected release doctor failure, got err=%v\n%s", err, out.String())
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	for _, check := range report.Checks {
		if check.Name == "update check" {
			if check.Status != doctor.Fail || !containsString(check.Blocks, "release") {
				t.Fatalf("update check = %+v", check)
			}
			return
		}
	}
	t.Fatalf("missing update check: %#v", report.Checks)
}

func hasDoctorCheck(report doctor.Report, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name {
			return true
		}
	}
	return false
}

func containsString(vals []string, want string) bool {
	for _, val := range vals {
		if val == want {
			return true
		}
	}
	return false
}

func withDotenvDoctor(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
	old := doctorOptionsHook
	doctorOptionsHook = func(opts *doctor.Options) {
		opts.PreferDotenv = true
	}
	t.Cleanup(func() { doctorOptionsHook = old })
}

func executeRootAllowError(t *testing.T, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := Root()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	return out, errOut, cmd.Execute()
}
