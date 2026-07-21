package goose

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
	"github.com/gongahkia/onibi/internal/store"
)

func TestLiveGooseDeny(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_GOOSE") != "1" || os.Getenv("ONIBI_LIVE_GOOSE_ALLOW_RUN") != "1" {
		t.Skip("set ONIBI_LIVE_GOOSE=1 and ONIBI_LIVE_GOOSE_ALLOW_RUN=1")
	}
	provider := strings.TrimSpace(os.Getenv("ONIBI_LIVE_GOOSE_PROVIDER"))
	model := strings.TrimSpace(os.Getenv("ONIBI_LIVE_GOOSE_MODEL"))
	if provider == "" || model == "" {
		t.Skip("set ONIBI_LIVE_GOOSE_PROVIDER and ONIBI_LIVE_GOOSE_MODEL")
	}
	bin := strings.TrimSpace(os.Getenv("ONIBI_GOOSE_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal(err)
		}
	}
	envs := []string{"ONIBI_LIVE_GOOSE", "ONIBI_LIVE_GOOSE_ALLOW_RUN", "ONIBI_LIVE_GOOSE_PROVIDER", "ONIBI_LIVE_GOOSE_MODEL", "ONIBI_GOOSE_BIN"}
	rec, err := liveartifact.New("goose", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	version, err := exec.Command(bin, "--version").Output()
	if err != nil {
		rec.Error("version", err)
		t.Fatal(err)
	}
	rec.Record("version", map[string]any{"value": strings.TrimSpace(string(version))})

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "plugins", "onibi", "hooks", "hooks.json")
	t.Setenv("ONIBI_GOOSE_HOOKS", path)
	record := filepath.Join(dir, "notify.log")
	notify := filepath.Join(dir, "onibi-notify")
	notifyBody := `#!/bin/sh
cat >/dev/null
printf '%s\n' "$*" >> "$ONIBI_LIVE_GOOSE_RECORD"
case " $* " in
  *" --type approval_request "*) printf '%s\n' "live deny fixture" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(notify, []byte(notifyBody), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "onibi-live-denied.txt")
	message := "Use a file-writing tool to create exactly " + target + " with content onibi-live-denied."
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--text", message, "--no-session", "--no-profile", "--with-builtin", "developer", "--provider", provider, "--model", model, "--output-format", "json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ONIBI_SESSION_ID=live-goose", "ONIBI_LIVE_GOOSE_RECORD="+record)
	if _, err := cmd.CombinedOutput(); err != nil {
		rec.Error("run", err)
	}
	body, err := os.ReadFile(record)
	if err != nil {
		rec.Error("approval-record", err)
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "--type approval_request") {
		rec.Record("approval-request", map[string]any{"ok": false})
		t.Fatal("Goose did not invoke the approval hook")
	}
	rec.Record("approval-request", map[string]any{"ok": true})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		rec.Record("denied-write", map[string]any{"ok": false})
		t.Fatalf("denied write exists: %v", err)
	}
	rec.Record("denied-write", map[string]any{"ok": true})
}
