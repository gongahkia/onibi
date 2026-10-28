package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/spf13/cobra"
)

func webDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Inspect host and terminal readiness without reading terminals or changing focus", Args: cobra.NoArgs, RunE: runWebDoctor}
	cmd.Flags().Bool("json", false, "print structured diagnostics")
	cmd.Flags().Bool("probe", false, "send a bounded read-only inventory request (may prompt for Automation permission)")
	cmd.Flags().String("read-target", "", "with --probe, verify output capture for this exact pane without printing its text")
	cmd.Flags().String("origin", "", "validate a private HTTPS origin without contacting it")
	cmd.Flags().String("listen", "127.0.0.1:8787", "validate the configured loopback listener")
	return cmd
}

func runWebDoctor(cmd *cobra.Command, _ []string) error {
	probe, _ := cmd.Flags().GetBool("probe")
	target, _ := cmd.Flags().GetString("read-target")
	if target != "" && !probe {
		return fmt.Errorf("--read-target requires --probe; passive diagnostics never read terminal text")
	}
	ctx, cancel := context.WithTimeout(contextOrBackground(cmd.Context()), 45*time.Second)
	defer cancel()
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	report := inspectWebHost(ctx, paths, runtime.GOOS)
	listen, _ := cmd.Flags().GetString("listen")
	if err := validateWebListen(listen); err != nil {
		report.Checks = append(report.Checks, ghostty.Check{ID: "listener", State: "invalid", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, ghostty.Check{ID: "listener", State: "configured", Detail: "Loopback address is valid. Listener availability is not tested."})
	}
	origin, _ := cmd.Flags().GetString("origin")
	if origin == "" {
		report.Checks = append(report.Checks, ghostty.Check{ID: "origin", State: "unknown", Detail: "No HTTPS origin supplied. Phone access needs the Tailscale Serve origin."})
	} else if err := web.ValidateOrigin(origin); err != nil {
		report.Checks = append(report.Checks, ghostty.Check{ID: "origin", State: "invalid", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, ghostty.Check{ID: "origin", State: "configured", Detail: "Origin syntax is valid. HTTPS reachability and tailnet access are not tested."})
	}
	if probe && runtime.GOOS != "darwin" {
		report.Checks = append(report.Checks, ghostty.Check{ID: "native_probe", State: "unsupported", Detail: "--probe and --read-target test native Ghostty control on macOS only. Use onibi session list for persistent sessions."})
	}
	if probe && runtime.GOOS == "darwin" {
		client := ghostty.New()
		targets, err := client.Targets(ctx)
		check := ghostty.Check{ID: "automation", State: "ready", Detail: fmt.Sprintf("Inventory responded with %d panes. No input was sent or focus changed.", len(targets))}
		if err != nil {
			check.State = ghostty.ErrorCode(err)
			check.Detail = "Inventory probe failed. " + err.Error()
		}
		for i := range report.Checks {
			if report.Checks[i].ID == "automation" {
				report.Checks[i] = check
			}
		}
		if target != "" {
			found := false
			for _, t := range targets {
				if t.ID == target {
					found = true
				}
			}
			capture := ghostty.Check{ID: "capture_probe", State: "target_changed", Detail: "The explicit target was not found in the current inventory."}
			if found {
				_, err := client.Snapshot(ctx, target)
				if err == nil {
					capture.State = "ready"
					capture.Detail = "Focused-pane capture succeeded. Captured text was discarded."
				} else {
					capture.State = ghostty.ErrorCode(err)
					capture.Detail = err.Error()
				}
			}
			report.Checks = append(report.Checks, capture)
		}
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	return writeWebDoctor(cmd, report, asJSON)
}

func writeWebDoctor(cmd *cobra.Command, report ghostty.Diagnostics, asJSON bool) error {
	if asJSON {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
			return err
		}
	} else {
		ui := terminalUI(cmd)
		ui.Banner("Host readiness")
		if report.Capabilities.Version != "" {
			ui.Field("Ghostty", report.Capabilities.Version)
		}
		for _, check := range report.Checks {
			ui.Line("  [%s] %s", check.State, check.ID)
			ui.Note("  " + check.Detail)
			ui.Line("")
		}
		ui.Note("Unknown checks are not evidence of readiness. No focus changes or terminal input are performed.")
	}
	for _, c := range report.Checks {
		switch c.State {
		case "ready", "configured", "running", "unknown", "not_applicable":
		default:
			return fmt.Errorf("host readiness needs attention; see diagnostics")
		}
	}
	return nil
}
