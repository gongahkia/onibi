package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusJSONIsValid(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--json", "--color", "never")
	var report cliStatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.Version == "" || report.Paths.StateDir == "" {
		t.Fatalf("incomplete report: %+v", report)
	}
	if len(report.Next) == 0 {
		t.Fatalf("missing next actions: %+v", report)
	}
}

func TestStatusTextShowsOverview(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--color", "never")
	got := out.String()
	for _, want := range []string{"Status", "daemon", "integrations", "Paths", "next"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status missing %q:\n%s", want, got)
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
