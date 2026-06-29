package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/updatecheck"
)

func withFakeUpdateCheck(t *testing.T, fn func() updatecheck.Result) *int {
	t.Helper()
	old := updateCheckRun
	calls := 0
	updateCheckRun = func(context.Context, updatecheck.Options) updatecheck.Result {
		calls++
		return fn()
	}
	t.Cleanup(func() { updateCheckRun = old })
	return &calls
}

func TestStatusUpdateCheckUsesCacheAndRefresh(t *testing.T) {
	withDefaultState(t)
	calls := withFakeUpdateCheck(t, func() updatecheck.Result {
		return updatecheck.Result{Status: updatecheck.StatusCurrent, Source: updatecheck.SourceGitHub, Detail: "ok"}
	})
	executeRoot(t, "status", "--json", "--color", "never")
	executeRoot(t, "status", "--json", "--color", "never")
	if *calls != 1 {
		t.Fatalf("update checks = %d want 1", *calls)
	}
	executeRoot(t, "status", "--json", "--refresh-update", "--color", "never")
	if *calls != 2 {
		t.Fatalf("update checks after refresh = %d want 2", *calls)
	}
}

func TestStatusUpdateCheckIgnoresDifferentBuildCache(t *testing.T) {
	withDefaultState(t)
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	stale := updatecheck.Result{
		Status:         updatecheck.StatusCurrent,
		Source:         updatecheck.SourceGitHub,
		CurrentVersion: "v0.0.0",
		CurrentCommit:  "old",
		Detail:         "stale",
	}
	b, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.KVSet(context.Background(), updateCheckCacheKey, b, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	calls := withFakeUpdateCheck(t, func() updatecheck.Result {
		return updatecheck.Result{Status: updatecheck.StatusCurrent, Source: updatecheck.SourceGitHub, Detail: "fresh"}
	})
	out, _ := executeRoot(t, "status", "--json", "--color", "never")
	if *calls != 1 {
		t.Fatalf("update checks = %d want 1", *calls)
	}
	var report cliStatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.Update == nil || report.Update.Detail != "fresh" {
		t.Fatalf("update summary = %+v", report.Update)
	}
}

func TestStatusRefreshUpdateUsesConditionalCache(t *testing.T) {
	withDefaultState(t)
	old := updateCheckRun
	calls := 0
	updateCheckRun = func(_ context.Context, opts updatecheck.Options) updatecheck.Result {
		calls++
		if calls == 2 {
			if opts.ConditionalETag != `"abc"` || opts.ConditionalLastModified != "Mon, 01 Jan 2024 00:00:00 GMT" || opts.CachedResult == nil {
				t.Fatalf("conditional opts = %+v cached=%+v", opts, opts.CachedResult)
			}
		}
		return updatecheck.Result{
			Status:       updatecheck.StatusCurrent,
			Source:       updatecheck.SourceGitHub,
			Detail:       "ok",
			ETag:         `"abc"`,
			LastModified: "Mon, 01 Jan 2024 00:00:00 GMT",
		}
	}
	t.Cleanup(func() { updateCheckRun = old })
	executeRoot(t, "status", "--json", "--color", "never")
	executeRoot(t, "status", "--json", "--refresh-update", "--color", "never")
	if calls != 2 {
		t.Fatalf("calls = %d want 2", calls)
	}
}

func TestStatusJSONIsValid(t *testing.T) {
	withFakeUpdateCheck(t, func() updatecheck.Result {
		return updatecheck.Result{Status: updatecheck.StatusCurrent, Source: updatecheck.SourceGitHub, Detail: "ok"}
	})
	withDefaultState(t)
	appendDefaultAudit(t, "notify.gotify.error", "failed")
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
	if report.Notify.Recent != 1 || report.Notify.Errors != 1 {
		t.Fatalf("notify summary = %+v", report.Notify)
	}
	if report.Update == nil || report.Update.Status == "" {
		t.Fatalf("missing update summary: %+v", report.Update)
	}
}

func TestStatusTextShowsOverview(t *testing.T) {
	withFakeUpdateCheck(t, func() updatecheck.Result {
		return updatecheck.Result{Status: updatecheck.StatusCurrent, Source: updatecheck.SourceGitHub, Detail: "ok"}
	})
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--color", "never")
	got := out.String()
	for _, want := range []string{"Status", "daemon", "notify", "integrations", "Paths", "next"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status missing %q:\n%s", want, got)
		}
	}
}

func TestStatusCompactIsOneLine(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--compact", "--no-doctor", "--no-hooks", "--no-update", "--color", "never")
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

func TestStatusNoUpdateOmitsUpdateSummary(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "status", "--json", "--no-update", "--color", "never")
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if _, ok := raw["update"]; ok {
		t.Fatalf("update present with --no-update:\n%s", out.String())
	}
}

func TestStatusNextActionsIncludeUpdateChecks(t *testing.T) {
	report := cliStatusReport{
		Daemon:  cliStatusProbe{Status: "PASS"},
		Devices: cliStatusCount{Active: 1},
		Update:  &cliUpdateSummary{Status: "outdated", Source: "local"},
	}
	got := strings.Join(statusNextActions(report), "\n")
	for _, want := range []string{"onibi update-check", "onibi doctor --after-upgrade --offline"} {
		if !strings.Contains(got, want) {
			t.Fatalf("next actions missing %q: %q", want, got)
		}
	}
}
