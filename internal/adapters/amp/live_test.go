package amp

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

func TestLiveAmpDeny(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_AMP") != "1" || os.Getenv("ONIBI_LIVE_AMP_ALLOW_EXECUTE") != "1" {
		t.Skip("set ONIBI_LIVE_AMP=1 and ONIBI_LIVE_AMP_ALLOW_EXECUTE=1")
	}
	if strings.TrimSpace(os.Getenv("AMP_API_KEY")) == "" {
		t.Skip("set AMP_API_KEY for an isolated authenticated Amp account")
	}
	bin := strings.TrimSpace(os.Getenv("ONIBI_AMP_BIN"))
	if bin == "" {
		var err error
		bin, err = exec.LookPath("amp")
		if err != nil {
			t.Fatal(err)
		}
	}
	envs := []string{"ONIBI_LIVE_AMP", "ONIBI_LIVE_AMP_ALLOW_EXECUTE", "ONIBI_AMP_BIN", "AMP_API_KEY"}
	rec, err := liveartifact.New("amp", envs...)
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
	plugin := filepath.Join(dir, ".amp", "plugins", "onibi.ts")
	record := filepath.Join(dir, "notify.log")
	notify := filepath.Join(dir, "onibi-notify")
	notifyBody := `#!/bin/sh
cat >/dev/null
printf '%s\n' "$*" >> "$ONIBI_LIVE_AMP_RECORD"
case " $* " in
  *" --type approval_request "*) printf '{"decision":"deny","reason":"live deny fixture"}\n' ;;
esac
`
	if err := os.WriteFile(notify, []byte(notifyBody), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"amp.dangerouslyAllowAll":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	t.Setenv("ONIBI_AMP_PLUGIN", plugin)
	if err := Install(context.Background(), db, notify); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "onibi-live-denied.txt")
	message := "Use a file-writing tool to create exactly " + target + " with content onibi-live-denied."
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--execute", message, "--settings-file", settings, "--no-archive-after-execute")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ONIBI_SESSION_ID=live-amp", "ONIBI_LIVE_AMP_RECORD="+record)
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
		t.Fatal("Amp did not invoke the approval plugin")
	}
	rec.Record("approval-request", map[string]any{"ok": true})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		rec.Record("denied-write", map[string]any{"ok": false})
		t.Fatalf("denied write exists: %v", err)
	}
	rec.Record("denied-write", map[string]any{"ok": true})
}
