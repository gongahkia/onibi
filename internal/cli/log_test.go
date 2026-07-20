package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func TestRunLogJSON(t *testing.T) {
	seedDefaultAudit(t, "approval.decided")

	var out bytes.Buffer
	cmd := logCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--json", "--n", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSON line, got %d: %q", len(lines), out.String())
	}
	var got store.AuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "approval.decided" || got.SessionID != "sess1" || got.DecidedByChat != 9999 {
		t.Fatalf("entry = %+v", got)
	}
	if strings.Contains(lines[0], `"Action"`) || !strings.Contains(lines[0], `"action"`) {
		t.Fatalf("json tags not applied: %s", lines[0])
	}
}

func TestRunLogExportUnaffectedByJSONFlag(t *testing.T) {
	seedDefaultAudit(t, "approval.decided")

	exportPath := filepath.Join(t.TempDir(), "audit.json")
	var out bytes.Buffer
	cmd := logCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--json", "--export", exportPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
	b, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	var got []store.AuditEntry
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Action != "approval.decided" {
		t.Fatalf("export = %+v", got)
	}
}

func TestRunLogNotifyFilter(t *testing.T) {
	seedDefaultAudit(t, "approval.decided")
	appendDefaultAudit(t, "notify.pushover.sent", "sent")
	var out bytes.Buffer
	cmd := logCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--notify", "--json", "--n", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "notify.pushover.sent") || strings.Contains(got, "approval.decided") {
		t.Fatalf("notify filter output = %s", got)
	}
}

func seedDefaultAudit(t *testing.T, action string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	db, err := openDefaultDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AuditAppend(context.Background(), action, "sess1", `{"x":1}`, 9999, "approved by user"); err != nil {
		t.Fatal(err)
	}
}

func appendDefaultAudit(t *testing.T, action, detail string) {
	t.Helper()
	db, err := openDefaultDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AuditAppend(context.Background(), action, "sess1", "", 0, detail); err != nil {
		t.Fatal(err)
	}
}
