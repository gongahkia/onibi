package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusJSONIsValid(t *testing.T) {
	withDefaultState(t)
	appendDefaultAudit(t, "notify.webpush.error", "failed")
	out, _ := executeRoot(t, "status", "--json", "--color", "never")
	var report cliStatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.Version == "" || report.Paths.StateDir == "" || len(report.Next) == 0 {
		t.Fatalf("incomplete report: %+v", report)
	}
	if report.Notify.Recent != 1 || report.Notify.Errors != 1 {
		t.Fatalf("notify summary = %+v", report.Notify)
	}
}

func TestStatusTextShowsOverview(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--color", "never")
	for _, want := range []string{"Status", "daemon", "notify", "integrations", "Paths", "next"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatusCompactIsOneLine(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--compact", "--no-doctor", "--no-hooks", "--color", "never")
	got := strings.TrimSpace(out.String())
	if strings.Count(got, "\n") != 0 {
		t.Fatalf("compact status should be one line:\n%s", got)
	}
	for _, want := range []string{"daemon=", "sessions=", "next="} {
		if !strings.Contains(got, want) {
			t.Fatalf("compact status missing %q: %s", want, got)
		}
	}
}

func TestStatusRejectsRemovedUpdateFlags(t *testing.T) {
	withDefaultState(t)
	if _, _, err := executeRootAllowError(t, "status", "--no-update", "--color", "never"); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err = %v", err)
	}
}
