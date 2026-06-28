package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/doctor"
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
