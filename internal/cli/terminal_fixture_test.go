package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/termui"
	"github.com/spf13/cobra"
)

// Opt-in visual fixture: no HTTP listener, state database, Ghostty, or service.
// A PTY can exercise the real renderer with known synthetic events.
func TestTerminalPresentationFixture(t *testing.T) {
	mode := os.Getenv("ONIBI_TERMINAL_FIXTURE")
	if mode == "" {
		t.Skip("opt-in simulated terminal presentation")
	}
	if mode == "logs" {
		path := filepath.Join(t.TempDir(), "web.log")
		if err := os.WriteFile(path, []byte(`{"time":"2026-01-01T12:58:04Z","level":"INFO","msg":"web.started","pid":1234}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := webLogsCmd()
		cmd.SetOut(os.Stdout)
		cmd.SetErr(os.Stderr)
		cmd.Flags().Set("follow", "true")
		ctx, cancel := context.WithTimeout(t.Context(), 2200*time.Millisecond)
		defer cancel()
		cmd.SetContext(ctx)
		if err := runWebLogs(cmd, path); err != nil {
			t.Fatal(err)
		}
		return
	}
	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	writeWebWelcome(cmd, "http://127.0.0.1:8787/#token=visual-fixture-only", "127.0.0.1:8787", "http://127.0.0.1:8787", "/example/onibi/logs/web.log")
	live := termui.NewLive(terminalUI(cmd))
	defer live.Close()
	live.Start(func(elapsed time.Duration) string {
		return live.StatusLine("HTTP live", elapsed, "3 req / 0 active", "Ctrl-C stops")
	})
	logger := logging.Web(io.Discard, live, slog.LevelInfo)
	logger.Info("web.started", "pid", 1234, "version", "preview")
	time.Sleep(350 * time.Millisecond)
	logger.Info("web.native_state", "state", "ready", "duration_ms", 18)
	time.Sleep(350 * time.Millisecond)
	logger.Info("web.request", "request_id", "preview-1", "method", "GET", "route", "/api/targets", "status", 200, "duration_ms", 20)
	time.Sleep(1500 * time.Millisecond)
}
