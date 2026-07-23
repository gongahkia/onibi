package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func runHooks(cmd *cobra.Command, args []string) error {
	action, err := selectedActionFlag(cmd, "show", "matrix")
	if err != nil {
		return err
	}
	switch action {
	case "show":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runHooksShow(cmd, args)
	case "matrix":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runHooksMatrix(cmd, args)
	default:
		return showActionHelp(cmd, args, "show", "matrix")
	}
}

// runInstallHooks implements `onibi install-hooks --agent <name>`.
func runInstallHooks(cmd *cobra.Command, _ []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	all, _ := cmd.Flags().GetBool("all")
	interactive, _ := cmd.Flags().GetBool("interactive")
	uninstall, _ := cmd.Flags().GetBool("uninstall")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := openDefaultDBForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := adapters.LoadExternalManifests(); err != nil {
		return err
	}
	notifyBin := ""
	if dryRun {
		notifyBin = "/usr/bin/onibi-notify"
	} else if !uninstall {
		notifyBin, err = locateNotifyBinary()
		if err != nil {
			return err
		}
	}
	if interactive {
		return runDetectedHooks(cmd, db, notifyBin, uninstall, true, dryRun)
	}
	if all {
		if uninstall {
			if dryRun {
				for _, name := range adapters.Names() {
					if err := printAgentHookPlan(cmd, db, name, true); err != nil {
						return err
					}
				}
				return nil
			}
			for _, name := range adapters.Names() {
				if err := installOneAgent(cmd, db, notifyBin, name, true); err != nil {
					return err
				}
			}
			return nil
		}
		return runDetectedHooks(cmd, db, notifyBin, false, false, dryRun)
	}
	if agent == "" {
		return runDetectedHooks(cmd, db, notifyBin, uninstall, true, dryRun)
	}
	if dryRun {
		return printAgentHookPlan(cmd, db, agent, uninstall)
	}
	return installOneAgent(cmd, db, notifyBin, agent, uninstall)
}
func runDetectedHooks(cmd *cobra.Command, db *store.DB, notifyBin string, uninstall, prompt, dryRun bool) error {
	targets, err := detectedHookTargets(cmd, db)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("no detected agent config dirs; use --agent or create a supported config dir")
	}
	if dryRun {
		for _, target := range targets {
			printHookTargetPlan(cmd, target, uninstall)
		}
		return nil
	}
	br := bufio.NewReader(cmd.InOrStdin())
	for _, target := range targets {
		if prompt {
			fmt.Fprintf(cmd.OutOrStdout(), "%s agent %s hook? [Y/n] ", action(uninstall), target.Name)
			line, _ := br.ReadString('\n')
			if strings.EqualFold(strings.TrimSpace(line), "n") {
				continue
			}
		}
		if err := installOneAgent(cmd, db, notifyBin, target.Name, uninstall); err != nil {
			return err
		}
	}
	return nil
}

type hookTarget struct {
	Kind string
	Name string
	Path string
}

func detectedHookTargets(cmd *cobra.Command, db *store.DB) ([]hookTarget, error) {
	var targets []hookTarget
	for _, name := range adapters.DetectedNames() {
		target, err := agentHookTarget(cmd, db, name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}
func agentHookTarget(cmd *cobra.Command, db *store.DB, name string) (hookTarget, error) {
	a, ok := adapters.Get(name)
	if !ok {
		return hookTarget{}, adapters.Unsupported(name)
	}
	info := a.Status(cmd.Context(), db)
	return hookTarget{Kind: "agent", Name: a.Name, Path: info.InstallPath}, nil
}

func printAgentHookPlan(cmd *cobra.Command, db *store.DB, name string, uninstall bool) error {
	target, err := agentHookTarget(cmd, db, name)
	if err != nil {
		return err
	}
	printHookTargetPlan(cmd, target, uninstall)
	return nil
}

func printHookTargetPlan(cmd *cobra.Command, target hookTarget, uninstall bool) {
	verb := "Install"
	if uninstall {
		verb = "Uninstall"
	}
	path := valueOrDash(target.Path)
	fmt.Fprintf(cmd.OutOrStdout(), "[PLAN] %s %s %s hook: %s\n", verb, target.Kind, target.Name, path)
}

func runInteractiveHooks(cmd *cobra.Command, db *store.DB, notifyBin string, uninstall bool) error {
	return runDetectedHooks(cmd, db, notifyBin, uninstall, true, false)
}

func installOneAgent(cmd *cobra.Command, db *store.DB, notifyBin, name string, uninstall bool) error {
	a, ok := adapters.Get(name)
	if !ok {
		return adapters.Unsupported(name)
	}
	if uninstall {
		if err := a.Uninstall(cmd.Context(), db); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled %s hooks\n", styleFor(cmd).green("[OK]"), a.Name)
		return nil
	}
	if err := a.Install(cmd.Context(), db, notifyBin); err != nil {
		return err
	}
	info := a.Status(cmd.Context(), db)
	fmt.Fprintf(cmd.OutOrStdout(), "%s Installed %s hooks into %s\n", styleFor(cmd).green("[OK]"), a.Name, info.InstallPath)
	if a.TrustInstructions != nil {
		for _, line := range a.TrustInstructions() {
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
	}
	return nil
}

type hooksShowReport struct {
	Agent             string                `json:"agent"`
	Support           string                `json:"support"`
	ConfigPath        string                `json:"config_path,omitempty"`
	ObservedVersion   string                `json:"observed_version,omitempty"`
	BundledVersion    string                `json:"bundled_version,omitempty"`
	VersionStatus     string                `json:"version_status,omitempty"`
	Record            *hookRecordView       `json:"record,omitempty"`
	BackupPath        string                `json:"backup_path,omitempty"`
	Preview           string                `json:"preview,omitempty"`
	ThresholdMS       *int64                `json:"threshold_ms,omitempty"`
	EditCommand       string                `json:"edit_command,omitempty"`
	Compatibility     []string              `json:"compatibility_notes,omitempty"`
	Expected          []common.ExpectedHook `json:"expected"`
	Observed          []common.ObservedHook `json:"observed"`
	Drift             []hookDrift           `json:"drift"`
	TrustInstructions []string              `json:"trust_instructions,omitempty"`
	Message           string                `json:"message,omitempty"`
}

type hookRecordView struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Version     string `json:"version,omitempty"`
	InstalledAt int64  `json:"installed_at"`
}

type hookDrift struct {
	Event            string `json:"event"`
	Matcher          string `json:"matcher,omitempty"`
	Status           string `json:"status"`
	ExpectedCommand  string `json:"expected_command,omitempty"`
	InstalledCommand string `json:"installed_command,omitempty"`
	Detail           string `json:"detail,omitempty"`
}

type hooksMatrixRow struct {
	Provider           string `json:"provider"`
	Support            string `json:"support"`
	InstallPath        string `json:"install_path,omitempty"`
	ObservedVersion    string `json:"observed_version,omitempty"`
	BundledVersion     string `json:"bundled_version"`
	TrustedManualStep  string `json:"trusted_manual_step"`
	ConfigSchemaStatus string `json:"config_schema_status"`
	HashStatus         string `json:"hash_status"`
	Drift              string `json:"drift"`
	NextAction         string `json:"next_action,omitempty"`
}

func runHooksShow(cmd *cobra.Command, _ []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	all, _ := cmd.Flags().GetBool("all")
	asJSON, _ := cmd.Flags().GetBool("json")
	if agent == "" && !all {
		return errors.New("--agent or --all required")
	}
	db, err := openDefaultDBForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	notifyBin := hooksShowNotifyBin()
	var reports []hooksShowReport
	if all {
		for _, name := range adapters.Names() {
			report, err := buildHooksShowReport(cmd, db, name, notifyBin)
			if err != nil {
				return err
			}
			reports = append(reports, report)
		}
	} else {
		report, err := buildHooksShowReport(cmd, db, agent, notifyBin)
		if err != nil {
			return err
		}
		reports = append(reports, report)
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if all {
			return enc.Encode(reports)
		}
		return enc.Encode(reports[0])
	}
	for i, report := range reports {
		if i > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
		}
		renderHooksShow(cmd, report)
	}
	return nil
}
func runHooksMatrix(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	db, err := openDefaultDBForCommand(cmd)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := buildHooksMatrix(cmd, db, hooksShowNotifyBin())
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "PROVIDER", "SUPPORT", "PATH", "OBSERVED", "BUNDLED", "MANUAL", "SCHEMA", "HASH", "DRIFT", "NEXT")}
	for _, r := range rows {
		table = append(table, []string{
			r.Provider,
			r.Support,
			valueOrDash(r.InstallPath),
			valueOrDash(r.ObservedVersion),
			valueOrDash(r.BundledVersion),
			valueOrDash(r.TrustedManualStep),
			r.ConfigSchemaStatus,
			r.HashStatus,
			r.Drift,
			valueOrDash(r.NextAction),
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func buildHooksMatrix(cmd *cobra.Command, db *store.DB, notifyBin string) ([]hooksMatrixRow, error) {
	var rows []hooksMatrixRow
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(cmd.Context(), db)
		report, err := buildHooksShowReport(cmd, db, name, notifyBin)
		if err != nil {
			return nil, err
		}
		rows = append(rows, matrixRowFromInfo(info, &report, "onibi install-hooks --agent "+name))
	}
	return rows, nil
}

func matrixRowFromInfo(info common.Info, report *hooksShowReport, installCmd string) hooksMatrixRow {
	row := hooksMatrixRow{
		Provider:           info.Name,
		Support:            info.Support,
		InstallPath:        info.InstallPath,
		ObservedVersion:    installedVersionString(info),
		BundledVersion:     info.BundledVersion,
		TrustedManualStep:  manualStep(report),
		ConfigSchemaStatus: schemaStatus(info, report),
		HashStatus:         hashStatus(info),
		Drift:              driftSummary(info, report),
		NextAction:         info.Next,
	}
	if row.NextAction == "" && !info.Installed {
		row.NextAction = installCmd
	}
	if row.NextAction == "" && row.TrustedManualStep != "none" {
		row.NextAction = row.TrustedManualStep
	}
	return row
}

func installedVersionString(info common.Info) string {
	if info.InstalledVersion == nil {
		return ""
	}
	return *info.InstalledVersion
}

func versionStatus(info common.Info) string {
	if !info.Installed {
		return "not installed"
	}
	if info.InstalledVersion == nil || *info.InstalledVersion == "" {
		return "unknown"
	}
	if info.Outdated || *info.InstalledVersion != info.BundledVersion {
		return "outdated"
	}
	return "ok"
}

func manualStep(report *hooksShowReport) string {
	if report == nil || len(report.TrustInstructions) == 0 {
		return "none"
	}
	return strings.Join(report.TrustInstructions, " | ")
}

func schemaStatus(info common.Info, report *hooksShowReport) string {
	if report != nil {
		for _, row := range report.Drift {
			if row.Status == "schema-invalid" {
				return "schema-invalid"
			}
		}
		if len(report.Expected) > 0 || len(report.Observed) > 0 {
			return "ok"
		}
	}
	if !info.Installed {
		return "not installed"
	}
	switch info.Name {
	case "amp", "opencode", "pi":
		return "owned-source"
	default:
		return "not checked"
	}
}

func hashStatus(info common.Info) string {
	switch {
	case !info.Installed:
		return "n/a"
	case !info.Managed:
		return "unmanaged"
	case info.Tampered:
		return "mismatch"
	case !info.HashRecorded:
		return "missing"
	case info.Adoptable:
		return "adoptable"
	default:
		return "ok"
	}
}

func driftSummary(info common.Info, report *hooksShowReport) string {
	if report == nil || len(report.Drift) == 0 {
		switch {
		case !info.Installed:
			return "not installed"
		case !info.Managed:
			return "unmanaged"
		case info.Tampered:
			return "hash-mismatch"
		case !info.HashRecorded:
			return "hash-missing"
		case info.Outdated:
			return "outdated"
		default:
			return "ok"
		}
	}
	counts := map[string]int{}
	for _, row := range report.Drift {
		counts[row.Status]++
	}
	if len(counts) == 1 && counts["ok"] > 0 {
		return "ok"
	}
	order := []string{"schema-invalid", "missing", "changed", "hash-missing", "hash-mismatch", "outdated", "extra", "ok"}
	parts := make([]string, 0, len(counts))
	for _, status := range order {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", status, counts[status]))
		}
	}
	return strings.Join(parts, ",")
}

func buildHooksShowReport(cmd *cobra.Command, db *store.DB, name, notifyBin string) (hooksShowReport, error) {
	a, ok := adapters.Get(name)
	if !ok {
		return hooksShowReport{}, adapters.Unsupported(name)
	}
	info := a.Status(cmd.Context(), db)
	report := hooksShowReport{
		Agent:           a.Name,
		Support:         info.Support,
		ConfigPath:      info.InstallPath,
		ObservedVersion: installedVersionString(info),
		BundledVersion:  info.BundledVersion,
		VersionStatus:   versionStatus(info),
		Expected:        []common.ExpectedHook{},
		Observed:        []common.ObservedHook{},
		Message:         info.Message,
	}
	if info.InstallPath != "" {
		if rec, ok, err := common.RecordFor(cmd.Context(), db, a.Name, info.InstallPath); err != nil {
			return hooksShowReport{}, err
		} else if ok {
			report.Record = &hookRecordView{Path: rec.Path, SHA256: rec.SHA256, Version: rec.Version, InstalledAt: rec.InstalledAt}
		}
		if backup, ok, err := common.LatestBackup(cmd.Context(), db, a.Name, info.InstallPath); err != nil {
			return hooksShowReport{}, err
		} else if ok {
			report.BackupPath = backup.BackupPath
		}
	}
	if a.BackupPath != nil && report.BackupPath == "" {
		report.BackupPath = a.BackupPath(cmd.Context(), db)
	}
	if a.ExpectedHooks != nil {
		expected, err := a.ExpectedHooks(notifyBin)
		if err != nil {
			return hooksShowReport{}, err
		}
		report.Expected = expected
	}
	if a.ObservedHooks != nil {
		observed, err := a.ObservedHooks()
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return hooksShowReport{}, err
			}
		} else {
			report.Observed = observed
		}
	}
	if a.TrustInstructions != nil {
		report.TrustInstructions = a.TrustInstructions()
	}
	report.Drift = hookDriftRows(info, report.Expected, report.Observed)
	return report, nil
}

func hookDriftRows(info common.Info, expected []common.ExpectedHook, observed []common.ObservedHook) []hookDrift {
	var rows []hookDrift
	used := make(map[int]bool)
	for _, ob := range observed {
		for _, p := range ob.Problems {
			if strings.HasPrefix(p, "schema-invalid:") {
				rows = append(rows, hookDrift{Event: ob.Event, Matcher: ob.Matcher, Status: "schema-invalid", InstalledCommand: ob.Command, Detail: p})
			}
		}
	}
	for _, exp := range expected {
		idx := -1
		for i, ob := range observed {
			if used[i] || !ob.Managed || ob.Event != exp.Event {
				continue
			}
			idx = i
			break
		}
		row := hookDrift{Event: exp.Event, Matcher: exp.Matcher, ExpectedCommand: exp.Command}
		if idx < 0 {
			row.Status = "missing"
			rows = append(rows, row)
			continue
		}
		used[idx] = true
		ob := observed[idx]
		row.InstalledCommand = ob.Command
		switch {
		case ob.Command != exp.Command || ob.Matcher != exp.Matcher || ob.Type != exp.Type:
			row.Status = "changed"
		case !info.HashRecorded:
			row.Status = "hash-missing"
		case info.Tampered:
			row.Status = "hash-mismatch"
		case info.Outdated:
			row.Status = "outdated"
		default:
			row.Status = "ok"
		}
		rows = append(rows, row)
	}
	for i, ob := range observed {
		if used[i] || ob.Event == "*" || ob.Command == "" {
			continue
		}
		detail := "managed hook not expected"
		if !ob.Managed {
			detail = "user hook, not managed"
		}
		rows = append(rows, hookDrift{Event: ob.Event, Matcher: ob.Matcher, Status: "extra", InstalledCommand: ob.Command, Detail: detail})
	}
	return rows
}

func renderHooksShow(cmd *cobra.Command, report hooksShowReport) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "agent: %s\n", report.Agent)
	fmt.Fprintf(out, "support: %s\n", report.Support)
	fmt.Fprintf(out, "provider config: %s\n", valueOrDash(report.ConfigPath))
	fmt.Fprintf(out, "version: observed=%s bundled=%s status=%s\n", valueOrDash(report.ObservedVersion), valueOrDash(report.BundledVersion), valueOrDash(report.VersionStatus))
	if report.Message != "" {
		fmt.Fprintf(out, "message: %s\n", report.Message)
	}
	if report.Record != nil {
		fmt.Fprintf(out, "record: path=%s hash=%s version=%s\n", report.Record.Path, report.Record.SHA256, valueOrDash(report.Record.Version))
	} else {
		fmt.Fprintln(out, "record: -")
	}
	fmt.Fprintf(out, "backup: %s\n", valueOrDash(report.BackupPath))
	if len(report.TrustInstructions) > 0 {
		fmt.Fprintln(out, "trust:")
		for _, line := range report.TrustInstructions {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	if report.ThresholdMS != nil {
		fmt.Fprintf(out, "threshold_ms: %d\n", *report.ThresholdMS)
	}
	if report.EditCommand != "" {
		fmt.Fprintf(out, "edit: %s\n", report.EditCommand)
	}
	if len(report.Compatibility) > 0 {
		fmt.Fprintln(out, "compatibility:")
		for _, line := range report.Compatibility {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	if report.Preview != "" {
		fmt.Fprintln(out, "preview:")
		fmt.Fprint(out, report.Preview)
		if !strings.HasSuffix(report.Preview, "\n") {
			fmt.Fprintln(out)
		}
	}
	table := [][]string{tableHeader(styleFor(cmd), "EVENT", "MATCHER", "DRIFT", "EXPECTED", "INSTALLED", "DETAIL")}
	for _, row := range report.Drift {
		table = append(table, []string{
			valueOrDash(row.Event),
			valueOrDash(row.Matcher),
			row.Status,
			valueOrDash(row.ExpectedCommand),
			valueOrDash(row.InstalledCommand),
			valueOrDash(row.Detail),
		})
	}
	_ = renderTable(out, table)
}

func hooksShowNotifyBin() string {
	notifyBin, err := locateNotifyBinary()
	if err == nil {
		return notifyBin
	}
	return "onibi-notify"
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func action(uninstall bool) string {
	if uninstall {
		return "Uninstall"
	}
	return "Install"
}

// locateNotifyBinary finds onibi-notify next to the onibi binary (the most
// common install layout), falling back to PATH lookup, then to a same-dir
// dev build. We need an absolute path because hook scripts run in arbitrary
// cwd.
var locateNotifyBinary = locateNotifyBinaryImpl

func locateNotifyBinaryImpl() (string, error) {
	if env := os.Getenv("ONIBI_NOTIFY_BIN"); env != "" {
		if filepath.IsAbs(env) {
			return env, nil
		}
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	// next to onibi
	self, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(self), "onibi-notify")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// PATH
	if p, err := exec.LookPath("onibi-notify"); err == nil {
		abs, _ := filepath.Abs(p)
		return abs, nil
	}
	return "", errors.New("onibi-notify binary not found — install with `make install` or set ONIBI_NOTIFY_BIN")
}
