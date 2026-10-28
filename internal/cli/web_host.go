package cli

import (
	"context"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/mux"
)

// inspectWebHost never captures text, changes focus, starts a multiplexer or
// installs a service. Ghostty is optional on Linux, not a failed prerequisite.
func inspectWebHost(ctx context.Context, paths config.Paths, platform string) ghostty.Diagnostics {
	report := ghostty.Diagnostics{CheckedAt: time.Now().UTC()}
	if platform == "darwin" {
		report = ghostty.Inspect(ctx)
	} else {
		report.Checks = append(report.Checks, ghostty.Check{ID: "ghostty", State: "not_applicable", Detail: "Native Ghostty control is macOS-only. Persistent sessions work on Linux, including WSL."})
	}
	report.Checks = append(report.Checks, ghostty.Check{ID: "platform", State: "ready", Detail: platform})
	cfg, _, err := config.Load(paths)
	check := ghostty.Check{ID: "multiplexer", State: "ready"}
	if err == nil {
		var kind string
		_, kind, err = mux.Choose("", cfg.Multiplexer, "")
		check.Detail = kind + " is configured. Starting a program has not been tested."
	}
	if err != nil {
		check.State, check.Detail = "unavailable", err.Error()
		if platform == "darwin" {
			check.State = "unknown" // Ghostty-only users do not need a multiplexer.
		}
	}
	report.Checks = append(report.Checks, check)
	check = ghostty.Check{ID: "sessions_daemon", State: "unknown", Detail: "No sessions daemon responded. onibi web starts one automatically; no terminal output was read."}
	if response, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypePing}, 500*time.Millisecond); err == nil && response.Reason == "" {
		check.State, check.Detail = "running", "The sessions daemon responded. No terminal output was read."
	}
	report.Checks = append(report.Checks, check)
	return report
}
