package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func TestSupportBundleRequiresRedacted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, errOut := &strings.Builder{}, &strings.Builder{}
	cmd := Root()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"system", "support", "bundle"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--redacted required") {
		t.Fatalf("err=%v stdout=%s stderr=%s", err, out.String(), errOut.String())
	}
}

func TestSupportBundleRedactsSecretsPromptChatIDAndHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := supportBundleFixture(t, home)
	out, _ := executeRoot(t, "system", "support", "bundle", "--redacted", "--color", "never")
	body := out.String()
	for _, forbidden := range []string{
		home,
		"123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"secret prompt body",
		"9999",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("bundle leaked %q:\n%s", forbidden, body)
		}
	}
	for _, want := range []string{"[REDACTED]", "~", "doctor", "hook_matrix", "web", "schema_version"} {
		if !strings.Contains(body, want) {
			t.Fatalf("bundle missing %q:\n%s", want, body)
		}
	}
	var bundle supportBundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("json: %v\n%s", err, body)
	}
	if bundle.Paths["state"] == "" || !strings.HasPrefix(bundle.Paths["state"], "~") {
		t.Fatalf("paths not home-redacted: %+v", bundle.Paths)
	}
	if len(bundle.Audit) != 1 || bundle.Audit[0].DecidedByChat != "[REDACTED]" || bundle.Audit[0].Detail != "[REDACTED]" {
		t.Fatalf("audit not redacted: %+v", bundle.Audit)
	}
	if paths.StateDir == "" {
		t.Fatal("fixture unused")
	}
}

func TestSupportBundleCanIncludeChatID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	supportBundleFixture(t, home)
	out, _ := executeRoot(t, "system", "support", "bundle", "--redacted", "--include-chat-id", "--color", "never")
	var bundle supportBundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(bundle.Audit) != 1 || bundle.Audit[0].DecidedByChat != "9999" {
		t.Fatalf("actor id not included: %+v", bundle.Audit)
	}
}

func supportBundleFixture(t *testing.T, home string) config.Paths {
	t.Helper()
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AuditAppend(context.Background(), "prompt.sent", "s1", "secret prompt body", 9999, "prompt=secret prompt body"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.LogDir, 0o700); err != nil {
		t.Fatal(err)
	}
	notify := filepath.Join(home, "bin", "onibi-notify")
	if err := os.MkdirAll(filepath.Dir(notify), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notify, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONIBI_NOTIFY_BIN", notify)
	logLine := "level=INFO token=123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA prompt=\"secret prompt body\" cwd=" + filepath.Join(home, "repo")
	if err := os.WriteFile(filepath.Join(paths.LogDir, "onibi.log"), []byte(logLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
}
