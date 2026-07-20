package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveOpenCodeApprove(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_OPENCODE") != "1" || os.Getenv("ONIBI_LIVE_OPENCODE_ALLOW_AUTO") != "1" {
		t.Skip("set ONIBI_LIVE_OPENCODE=1 and ONIBI_LIVE_OPENCODE_ALLOW_AUTO=1")
	}
	model := strings.TrimSpace(os.Getenv("ONIBI_LIVE_OPENCODE_MODEL"))
	if model == "" {
		t.Skip("set ONIBI_LIVE_OPENCODE_MODEL to an authenticated provider/model")
	}
	bin := strings.TrimSpace(os.Getenv("ONIBI_OPENCODE_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("opencode")
		if err != nil {
			t.Fatal(err)
		}
	}
	envs := []string{"ONIBI_LIVE_OPENCODE", "ONIBI_LIVE_OPENCODE_ALLOW_AUTO", "ONIBI_LIVE_OPENCODE_MODEL", "ONIBI_OPENCODE_BIN"}
	rec, err := liveartifact.New("opencode", envs...)
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
	record := filepath.Join(dir, "notify.log")
	notify := filepath.Join(dir, "onibi-notify")
	notifyBody := `#!/bin/sh
cat >/dev/null
printf '%s\n' "$*" >> "$ONIBI_LIVE_OPENCODE_RECORD"
case " $* " in
  *" --type approval_request "*) printf '{"decision":"approve"}\n' ;;
esac
`
	if err := os.WriteFile(notify, []byte(notifyBody), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(dir, ".opencode", "plugins", "onibi.js")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte(pluginSource(notify)), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "onibi-live-approved.txt")
	message := "Use the write tool once to create exactly " + target + " with exactly this content: onibi-live-approved. Do not use any other tool."
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--auto", "--dir", dir, "--model", model, message)
	cmd.Env = append(os.Environ(), "ONIBI_SESSION_ID=live-opencode", "ONIBI_LIVE_OPENCODE_RECORD="+record)
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
		t.Fatal("OpenCode did not invoke the approval plugin")
	}
	rec.Record("approval-request", map[string]any{"ok": true})
	content, err := os.ReadFile(target)
	if err != nil {
		rec.Error("approved-write", err)
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "onibi-live-approved" {
		rec.Record("approved-write", map[string]any{"ok": false})
		t.Fatal("approved write content mismatch")
	}
	rec.Record("approved-write", map[string]any{"ok": true})
}
