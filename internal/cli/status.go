package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

type cliStatusReport struct {
	Version      string              `json:"version"`
	Commit       string              `json:"commit"`
	Date         string              `json:"date"`
	Config       cliStatusConfig     `json:"config"`
	Paths        cliStatusPaths      `json:"paths"`
	Daemon       cliStatusProbe      `json:"daemon"`
	Sessions     cliStatusCount      `json:"sessions"`
	Devices      cliStatusCount      `json:"devices"`
	Integrations cliIntegrationCount `json:"integrations"`
	Notify       cliNotifySummary    `json:"notify"`
	Doctor       cliDoctorSummary    `json:"doctor"`
	Update       *cliUpdateSummary   `json:"update,omitempty"`
	Terminal     cliTerminalSummary  `json:"terminal"`
	Next         []string            `json:"next"`
}

type cliStatusConfig struct {
	ListenAddr string `json:"listen_addr"`
	Transport  string `json:"transport"`
	Shell      string `json:"shell"`
}

type cliStatusPaths struct {
	StateDir string `json:"state_dir"`
	DB       string `json:"db"`
	Socket   string `json:"socket"`
	Config   string `json:"config"`
}

type cliStatusProbe struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
	RTTMS  int64  `json:"rtt_ms,omitempty"`
}

type cliStatusCount struct {
	Active  int `json:"active"`
	Total   int `json:"total,omitempty"`
	Revoked int `json:"revoked,omitempty"`
}

type cliIntegrationCount struct {
	Installed int `json:"installed"`
	Detected  int `json:"detected"`
	Issues    int `json:"issues"`
	Total     int `json:"total"`
}

type cliDoctorSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

type cliNotifySummary struct {
	Recent     int    `json:"recent"`
	Errors     int    `json:"errors"`
	LastAction string `json:"last_action,omitempty"`
	LastDetail string `json:"last_detail,omitempty"`
	LastAt     string `json:"last_at,omitempty"`
}

type cliUpdateSummary struct {
	Status  string `json:"status"`
	Source  string `json:"source"`
	Detail  string `json:"detail"`
	Command string `json:"command,omitempty"`
}

type cliTerminalSummary struct {
	Default string                   `json:"default"`
	Ghostty daemon.GhosttyCapability `json:"ghostty"`
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"overview"},
		Short:   "Show daemon, devices, sessions, hooks, and doctor summary",
		RunE:    runStatus,
	}
	cmd.Flags().Bool("json", false, "print JSON")
	cmd.Flags().Bool("strict", false, "exit non-zero on daemon or doctor failure")
	cmd.Flags().Bool("watch", false, "refresh status until interrupted")
	cmd.Flags().Duration("interval", 2*time.Second, "watch refresh interval")
	cmd.Flags().Duration("timeout", 350*time.Millisecond, "daemon probe timeout")
	cmd.Flags().Bool("compact", false, "print one-line human output")
	cmd.Flags().Bool("no-doctor", false, "skip doctor summary")
	cmd.Flags().Bool("no-hooks", false, "skip integration scan")
	cmd.Flags().Bool("no-update", false, "skip update check")
	cmd.Flags().Bool("refresh-update", false, "ignore cached update check")
	return cmd
}

func runStatus(cmd *cobra.Command, _ []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	if watch {
		return runStatusWatch(cmd)
	}
	report, err := buildCLIStatus(cmd)
	if err != nil {
		return err
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		return statusStrictError(cmd, report)
	}
	if compactStatus(cmd) {
		renderCLIStatusCompact(cmd, report)
		return statusStrictError(cmd, report)
	}
	if err := renderCLIStatus(cmd, report); err != nil {
		return err
	}
	return statusStrictError(cmd, report)
}

func runStatusWatch(cmd *cobra.Command) error {
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return fmt.Errorf("--watch cannot be combined with --json")
	}
	interval, _ := cmd.Flags().GetDuration("interval")
	if interval <= 0 {
		return fmt.Errorf("--interval must be > 0")
	}
	for {
		report, err := buildCLIStatus(cmd)
		if err != nil {
			return err
		}
		if !quiet(cmd) {
			fmt.Fprint(cmd.OutOrStdout(), "\x1b[2J\x1b[H")
		}
		if compactStatus(cmd) {
			renderCLIStatusCompact(cmd, report)
		} else if err := renderCLIStatus(cmd, report); err != nil {
			return err
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(interval):
		}
	}
}

func buildCLIStatus(cmd *cobra.Command) (cliStatusReport, error) {
	paths, db, err := openCLIStore()
	if err != nil {
		return cliStatusReport{}, err
	}
	defer db.Close()
	cfg, _, err := config.Load(paths)
	if err != nil {
		return cliStatusReport{}, err
	}
	report := cliStatusReport{
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
		Date:    buildinfo.Date,
		Config: cliStatusConfig{
			ListenAddr: cfg.Web.ListenAddr,
			Transport:  cfg.Transport.Mode,
			Shell:      shellLabel(cfg),
		},
		Paths: cliStatusPaths{
			StateDir: paths.StateDir,
			DB:       paths.DBFile,
			Socket:   paths.Socket,
			Config:   paths.Config,
		},
		Terminal: cliTerminalSummary{
			Default: cfg.Terminal.Default,
			Ghostty: daemon.ProbeGhostty(cmd.Context()),
		},
	}
	report.Daemon = probeDaemon(cmd, paths)
	report.Sessions = countSessions(cmd, db)
	report.Devices = countDevices(cmd, db)
	report.Notify = summarizeNotify(cmd, db)
	if skipHooks, _ := cmd.Flags().GetBool("no-hooks"); !skipHooks {
		report.Integrations = countIntegrations(cmd, db)
	}
	if skipDoctor, _ := cmd.Flags().GetBool("no-doctor"); !skipDoctor {
		report.Doctor = summarizeDoctor(doctor.Run(cmd.Context(), doctor.Options{Paths: paths, Offline: true, Mode: "preflight"}))
	}
	if skipUpdate, _ := cmd.Flags().GetBool("no-update"); !skipUpdate {
		refreshUpdate, _ := cmd.Flags().GetBool("refresh-update")
		report.Update = summarizeUpdate(cachedUpdateCheck(cmd.Context(), db, refreshUpdate))
	}
	report.Next = statusNextActions(report)
	return report, nil
}

func renderCLIStatus(cmd *cobra.Command, report cliStatusReport) error {
	printCLIHeader(cmd, "Status")
	style := styleFor(cmd)
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s  commit=%s  date=%s\n\n", style.bold("onibi"), report.Version, report.Commit, report.Date)
	runtimeRows := [][]string{
		tableHeader(style, "RUNTIME", "STATUS", "DETAIL"),
		{"daemon", style.status(report.Daemon.Status), report.Daemon.Detail},
		{"web", style.status("INFO"), report.Config.ListenAddr},
		{"transport", style.status("INFO"), report.Config.Transport},
		{"shell", style.status("INFO"), report.Config.Shell},
		{"ghostty", style.status(statusStyleForGhostty(report.Terminal.Ghostty)), report.Terminal.Ghostty.Detail},
	}
	if report.Update != nil {
		runtimeRows = append(runtimeRows, []string{"update", style.status(statusStyleForUpdate(report.Update.Status)), report.Update.Detail})
	}
	if err := renderTable(cmd.OutOrStdout(), runtimeRows); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	countRows := [][]string{
		tableHeader(style, "SURFACE", "ACTIVE", "TOTAL", "DETAIL"),
		{"sessions", fmt.Sprint(report.Sessions.Active), fmt.Sprint(report.Sessions.Total), ""},
		{"devices", fmt.Sprint(report.Devices.Active), fmt.Sprint(report.Devices.Total), fmt.Sprintf("revoked=%d", report.Devices.Revoked)},
		{"notify", fmt.Sprint(report.Notify.Recent), fmt.Sprint(report.Notify.Recent), fmt.Sprintf("errors=%d last=%s", report.Notify.Errors, valueOrDash(report.Notify.LastAction))},
		{"integrations", fmt.Sprint(report.Integrations.Installed), fmt.Sprint(report.Integrations.Total), fmt.Sprintf("detected=%d issues=%d", report.Integrations.Detected, report.Integrations.Issues)},
		{"doctor", fmt.Sprint(report.Doctor.Pass), fmt.Sprint(report.Doctor.Pass + report.Doctor.Warn + report.Doctor.Fail), fmt.Sprintf("warn=%d fail=%d", report.Doctor.Warn, report.Doctor.Fail)},
	}
	if err := renderTable(cmd.OutOrStdout(), countRows); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Paths"))
	return renderTable(cmd.OutOrStdout(), [][]string{
		{"state", report.Paths.StateDir},
		{"db", report.Paths.DB},
		{"socket", report.Paths.Socket},
		{"config", report.Paths.Config},
		{"next", strings.Join(report.Next, " ; ")},
	})
}

func renderCLIStatusCompact(cmd *cobra.Command, report cliStatusReport) {
	update := "skipped"
	if report.Update != nil {
		update = report.Update.Status
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"daemon=%s sessions=%d devices=%d notify_recent=%d notify_errors=%d integrations=%d issues=%d doctor_warn=%d doctor_fail=%d update=%s next=%q\n",
		strings.ToLower(report.Daemon.Status),
		report.Sessions.Active,
		report.Devices.Active,
		report.Notify.Recent,
		report.Notify.Errors,
		report.Integrations.Installed,
		report.Integrations.Issues,
		report.Doctor.Warn,
		report.Doctor.Fail,
		update,
		strings.Join(report.Next, ";"),
	)
}

func probeDaemon(cmd *cobra.Command, paths config.Paths) cliStatusProbe {
	start := time.Now()
	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		timeout = 350 * time.Millisecond
	}
	resp, err := pingSocket(cmd.Context(), paths.Socket, timeout)
	rtt := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return cliStatusProbe{Status: "WARN", Detail: "not running; run onibi up", RTTMS: rtt.Milliseconds()}
	}
	detail := strings.TrimSpace(resp.Text)
	if detail == "" {
		detail = "pong"
	}
	return cliStatusProbe{Status: "PASS", Detail: detail, RTTMS: rtt.Milliseconds()}
}

func countSessions(cmd *cobra.Command, db *store.DB) cliStatusCount {
	active, err := db.SessionsActive(cmd.Context())
	if err != nil {
		return cliStatusCount{}
	}
	recent, err := db.SessionsRecent(cmd.Context(), 1000, true)
	if err != nil {
		return cliStatusCount{Active: len(active)}
	}
	return cliStatusCount{Active: len(active), Total: len(recent)}
}

func countDevices(cmd *cobra.Command, db *store.DB) cliStatusCount {
	all, err := db.ListWebSessions(cmd.Context(), true)
	if err != nil {
		return cliStatusCount{}
	}
	var active, revoked int
	for _, d := range all {
		if d.Revoked {
			revoked++
		} else {
			active++
		}
	}
	return cliStatusCount{Active: active, Total: len(all), Revoked: revoked}
}

func countIntegrations(cmd *cobra.Command, db *store.DB) cliIntegrationCount {
	var out cliIntegrationCount
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(cmd.Context(), db)
		out.Total++
		if info.Installed {
			out.Installed++
		}
		if agentDetected(name) {
			out.Detected++
		}
		if info.Installed && (info.Tampered || info.Outdated) {
			out.Issues++
		}
	}
	for _, name := range adapters.ShellNames() {
		info := adapters.ShellStatus(cmd.Context(), db, name)
		out.Total++
		if info.Installed {
			out.Installed++
		}
		if shellDetected(name) {
			out.Detected++
		}
		if info.Installed && (info.Tampered || info.Outdated) {
			out.Issues++
		}
	}
	return out
}

func summarizeDoctor(report doctor.Report) cliDoctorSummary {
	var out cliDoctorSummary
	for _, c := range report.Checks {
		switch c.Status {
		case doctor.Pass:
			out.Pass++
		case doctor.Warn:
			out.Warn++
		case doctor.Fail:
			out.Fail++
		}
	}
	return out
}

func summarizeUpdate(res updatecheck.Result) *cliUpdateSummary {
	return &cliUpdateSummary{
		Status:  string(res.Status),
		Source:  string(res.Source),
		Detail:  res.Detail,
		Command: res.Command,
	}
}

func statusStyleForUpdate(status string) string {
	switch updatecheck.Status(status) {
	case updatecheck.StatusCurrent:
		return "PASS"
	case updatecheck.StatusOutdated, updatecheck.StatusUnavailable:
		return "WARN"
	default:
		return "INFO"
	}
}

func statusStyleForGhostty(cap daemon.GhosttyCapability) string {
	if cap.Installed && cap.AppleScript {
		return "PASS"
	}
	return "INFO"
}

func summarizeNotify(cmd *cobra.Command, db *store.DB) cliNotifySummary {
	rows, err := db.AuditRecent(cmd.Context(), 200)
	if err != nil {
		return cliNotifySummary{}
	}
	var out cliNotifySummary
	for _, row := range rows {
		if !strings.HasPrefix(row.Action, "notify.") {
			continue
		}
		out.Recent++
		if strings.Contains(row.Action, ".error") || strings.Contains(row.Action, ".failed") {
			out.Errors++
		}
		if out.LastAction == "" {
			out.LastAction = row.Action
			out.LastDetail = row.Detail
			out.LastAt = row.TS.Format("2006-01-02T15:04:05Z07:00")
		}
	}
	return out
}

func statusNextActions(report cliStatusReport) []string {
	var next []string
	if report.Daemon.Status != "PASS" {
		next = append(next, "onibi up")
	}
	if report.Devices.Active == 0 {
		next = append(next, "onibi pair")
	}
	if report.Integrations.Total > 0 && report.Integrations.Installed == 0 {
		next = append(next, "onibi install-hooks --interactive")
	}
	if report.Doctor.Warn > 0 || report.Doctor.Fail > 0 {
		next = appendUnique(next, "onibi doctor --explain")
	}
	if report.Update != nil && report.Update.Status != string(updatecheck.StatusCurrent) {
		next = appendUnique(next, "onibi update-check")
		if report.Update.Source == string(updatecheck.SourceLocal) {
			next = appendUnique(next, "onibi doctor --after-upgrade --offline")
		}
	}
	if len(next) == 0 {
		next = append(next, "onibi up")
	}
	return next
}

func appendUnique(vals []string, v string) []string {
	for _, existing := range vals {
		if existing == v {
			return vals
		}
	}
	return append(vals, v)
}

func statusStrictError(cmd *cobra.Command, report cliStatusReport) error {
	strict, _ := cmd.Flags().GetBool("strict")
	if !strict {
		return nil
	}
	if report.Daemon.Status != "PASS" || report.Doctor.Fail > 0 {
		return fmt.Errorf("status check failed")
	}
	return nil
}

func compactStatus(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("compact")
	return v || quiet(cmd)
}

func shellLabel(cfg config.Config) string {
	out := cfg.Shell.Default
	if out == "" {
		out = "auto"
	}
	if cfg.Shell.Login {
		out += " login"
	}
	return out
}
