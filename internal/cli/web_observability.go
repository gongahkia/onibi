package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/termui"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/webadmin"
	"github.com/spf13/cobra"
)

func commandHint() string {
	exe, err := os.Executable()
	if err != nil {
		return "onibi"
	}
	if cwd, err := os.Getwd(); err == nil && exe == filepath.Join(cwd, "bin", "onibi") {
		return "./bin/onibi"
	}
	return "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
}

type terminalUIKey struct{}

func terminalUI(cmd *cobra.Command) *termui.UI {
	ctx := contextOrBackground(cmd.Context())
	if ui, ok := ctx.Value(terminalUIKey{}).(*termui.UI); ok {
		return ui
	}
	ui := terminalUIFor(cmd, cmd.OutOrStdout())
	cmd.SetContext(context.WithValue(ctx, terminalUIKey{}, ui))
	return ui
}
func terminalUIFor(cmd *cobra.Command, out io.Writer) *termui.UI {
	ui := termui.New(out)
	plain, _ := cmd.Flags().GetBool("plain")
	if plain {
		ui.Interactive = false
	}
	return ui
}
func webStatusCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Inspect the running server and cached native health without probing Ghostty", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		if !asJSON {
			terminalUI(cmd).Banner("Web cockpit / status")
		}
		response, err := webAdminCall(cmd, webadmin.Request{Operation: "status"})
		return writeWebStatusResult(cmd, response, err, asJSON)
	}}
	cmd.Flags().Bool("json", false, "print runtime state as JSON")
	return cmd
}
func writeWebStatusResult(cmd *cobra.Command, response webadmin.Response, err error, asJSON bool) error {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"running": false, "state": "stopped"})
		}
		ui := terminalUI(cmd)
		ui.Banner("Web cockpit / status")
		ui.Field("State", "Stopped")
		ui.Note("Onibi is not running. Ctrl-C in its terminal stops the server.")
		ui.Line("")
		ui.Field("Start", commandHint()+" web")
		ui.Field("Login service", commandHint()+" web service start")
		ui.Note("Keep Onibi open; run status in a second terminal.")
		return nil
	}
	if err != nil {
		if strings.Contains(err.Error(), "unknown administration operation") {
			return errors.New("the running Onibi version predates status; restart it after updating")
		}
		return err
	}
	if response.Status == nil {
		return errors.New("the running server does not support status; restart it after updating")
	}
	return writeWebStatus(cmd, *response.Status, asJSON)
}
func writeWebStatus(cmd *cobra.Command, s web.RuntimeStatus, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Running bool `json:"running"`
			web.RuntimeStatus
		}{true, s})
	}
	ui := terminalUI(cmd)
	ui.Banner("Web cockpit / status")
	ui.Field("State", "Running")
	ui.Field("Version", s.Version+" ("+s.Commit+")")
	ui.Field("Process", strconv.Itoa(s.PID)+" / "+(time.Duration(s.UptimeSeconds)*time.Second).String()+" uptime")
	ui.Field("Executable", s.Executable)
	ui.Field("Open", s.Origin)
	for _, entry := range []struct {
		name        string
		observation web.Observation
	}{{"Inventory", s.Inventory}, {"Capture", s.Capture}} {
		value := entry.observation.State
		if entry.observation.Code != "" {
			value += " / " + entry.observation.Code
		}
		if !entry.observation.CheckedAt.IsZero() {
			value += " / last observed " + entry.observation.CheckedAt.Local().Format(time.RFC3339)
		}
		ui.Field(entry.name, value)
	}
	ui.Field("Requests", fmt.Sprintf("%d total / %d active / %d HTTP errors", s.Requests, s.ActiveRequests, s.Errors))
	ui.Field("Actions", fmt.Sprintf("%d completed / %d rejected / %d uncertain", s.Completed, s.Rejected, s.Uncertain))
	ui.Note("Counters cover this process. Native health reflects completed observations.")
	if s.Commit != buildinfo.Commit {
		ui.Note("Running and local revisions differ. Restart explicitly when ready.")
	}
	return nil
}
func webLogsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "logs", Short: "Read private web telemetry; optionally follow rotation", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		paths, err := config.DefaultPaths()
		if err != nil {
			return err
		}
		return runWebLogs(cmd, filepath.Join(paths.LogDir, "web.log"))
	}}
	cmd.Flags().Int("tail", 50, "last records to display (0–1000, bounded to the last 1 MiB)")
	cmd.Flags().BoolP("follow", "f", false, "follow new records and reopen after rotation; Ctrl-C exits")
	cmd.Flags().Bool("json", false, "print original structured JSON records")
	return cmd
}

func runWebLogs(cmd *cobra.Command, logPath string) error {
	tail, _ := cmd.Flags().GetInt("tail")
	follow, _ := cmd.Flags().GetBool("follow")
	asJSON, _ := cmd.Flags().GetBool("json")
	ctx, stop := signal.NotifyContext(contextOrBackground(cmd.Context()), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ui := terminalUI(cmd)
	live := termui.NewLive(ui)
	defer live.Close()
	if !asJSON {
		ui.Banner("Web cockpit / logs")
		ui.Field("File", termui.ShortPath(logPath))
		if follow {
			ui.Field("View", "Following new events / Ctrl-C exits this viewer")
			ui.Note("Follow waits even when the server is stopped.")
			ui.Note("For a snapshot that returns immediately, omit --follow.")
		} else {
			ui.Field("View", fmt.Sprintf("Last %d records", tail))
		}
		ui.Line("")
		if follow {
			live.Start(func(elapsed time.Duration) string {
				return fmt.Sprintf("Following / %s / %d records / Ctrl-C exits", termui.Elapsed(elapsed), live.Records())
			})
		}
	}
	err := logging.ReadLogs(ctx, logPath, tail, follow, func(line []byte) error {
		if asJSON {
			if !json.Valid(line) {
				return errors.New("invalid JSON log record")
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), string(line))
			return err
		}
		_, err := fmt.Fprintln(live, logging.FormatRecord(line))
		return err
	})
	err = errors.Join(err, live.Close())
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no web log yet; start %s web first", commandHint())
	}
	if err == nil && follow && !asJSON {
		ui.Note("Log follow ended.")
	}
	return err
}
