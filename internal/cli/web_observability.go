package cli

import (
	"encoding/json"
	"errors"
	"fmt"
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
func terminalUI(cmd *cobra.Command) *termui.UI {
	ui := termui.New(cmd.OutOrStdout())
	plain, _ := cmd.Flags().GetBool("plain")
	if plain {
		ui.Interactive = false
	}
	return ui
}
func webStatusCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Inspect the running server and cached native health without probing Ghostty", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		response, err := webAdminCall(cmd, webadmin.Request{Operation: "status"})
		if err != nil {
			return err
		}
		if response.Status == nil {
			return errors.New("the running server does not support status; restart it after updating")
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		return writeWebStatus(cmd, *response.Status, asJSON)
	}}
	cmd.Flags().Bool("json", false, "print runtime state as JSON")
	return cmd
}
func writeWebStatus(cmd *cobra.Command, s web.RuntimeStatus, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(s)
	}
	ui := terminalUI(cmd)
	ui.Banner("Web cockpit / running")
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
		tail, _ := cmd.Flags().GetInt("tail")
		follow, _ := cmd.Flags().GetBool("follow")
		asJSON, _ := cmd.Flags().GetBool("json")
		ctx, stop := signal.NotifyContext(contextOrBackground(cmd.Context()), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		err = logging.ReadLogs(ctx, filepath.Join(paths.LogDir, "web.log"), tail, follow, func(line []byte) error {
			if asJSON {
				if !json.Valid(line) {
					return errors.New("invalid JSON log record")
				}
				_, err := fmt.Fprintln(cmd.OutOrStdout(), string(line))
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), logging.FormatRecord(line))
			return err
		})
		if os.IsNotExist(err) {
			return fmt.Errorf("no web log yet; start %s web first", commandHint())
		}
		return err
	}}
	cmd.Flags().Int("tail", 50, "last records to display (0–1000, bounded to the last 1 MiB)")
	cmd.Flags().BoolP("follow", "f", false, "follow new records and reopen after rotation; Ctrl-C exits")
	cmd.Flags().Bool("json", false, "print original structured JSON records")
	return cmd
}
