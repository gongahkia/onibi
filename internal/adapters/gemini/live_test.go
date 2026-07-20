package gemini

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

func TestLiveGeminiDeny(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_GEMINI") != "1" {
		t.Skip("set ONIBI_LIVE_GEMINI=1")
	}
	model := strings.TrimSpace(os.Getenv("ONIBI_LIVE_GEMINI_MODEL"))
	if model == "" {
		t.Skip("set ONIBI_LIVE_GEMINI_MODEL to an authenticated model")
	}
	bin := strings.TrimSpace(os.Getenv("ONIBI_GEMINI_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("gemini")
		if err != nil {
			t.Fatal(err)
		}
	}
	envs := []string{"ONIBI_LIVE_GEMINI", "ONIBI_LIVE_GEMINI_MODEL", "ONIBI_GEMINI_BIN"}
	rec, err := liveartifact.New("gemini", envs...)
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
	settings := filepath.Join(dir, ".gemini", "settings.json")
	record := filepath.Join(dir, "notify.log")
	notify := filepath.Join(dir, "onibi-notify")
	notifyBody := `#!/bin/sh
cat >/dev/null
printf '%s\n' "$*" >> "$ONIBI_LIVE_GEMINI_RECORD"
case " $* " in
  *" --type approval_request "*) printf '{"decision":"deny","reason":"live deny fixture"}\n' ;;
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
	t.Setenv("ONIBI_GEMINI_SETTINGS", settings)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "onibi-live-denied.txt")
	message := "Use a file-writing tool to create exactly " + target + " with content onibi-live-denied."
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--prompt", message, "--model", model, "--skip-trust", "--output-format", "json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ONIBI_SESSION_ID=live-gemini", "ONIBI_LIVE_GEMINI_RECORD="+record)
	if _, err := cmd.CombinedOutput(); err != nil {
		rec.Error("run", err)
		t.Fatal(err)
	}
	body, err := os.ReadFile(record)
	if err != nil {
		rec.Error("approval-record", err)
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "--type approval_request") {
		rec.Record("approval-request", map[string]any{"ok": false})
		t.Fatal("Gemini did not invoke the approval hook")
	}
	rec.Record("approval-request", map[string]any{"ok": true})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		rec.Record("denied-write", map[string]any{"ok": false})
		t.Fatalf("denied write exists: %v", err)
	}
	rec.Record("denied-write", map[string]any{"ok": true})
}
