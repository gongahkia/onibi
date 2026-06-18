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

// runInstallHooks implements `onibi install-hooks --agent <name>`.
func runInstallHooks(cmd *cobra.Command, _ []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	sh, _ := cmd.Flags().GetString("shell")
	all, _ := cmd.Flags().GetBool("all")
	interactive, _ := cmd.Flags().GetBool("interactive")
	uninstall, _ := cmd.Flags().GetBool("uninstall")

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	shellMinMS := cfg.Shell.MinDuration.Std().Milliseconds()
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()

	notifyBin := ""
	if !uninstall {
		notifyBin, err = locateNotifyBinary()
		if err != nil {
			return err
		}
	}

	if sh != "" {
		if uninstall {
			if err := adapters.UninstallShell(cmd.Context(), db, sh); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled %s shell hook\n", styleFor(cmd).green("[OK]"), sh)
			return nil
		}
		if err := adapters.InstallShell(cmd.Context(), db, notifyBin, sh, shellMinMS); err != nil {
			return err
		}
		info := adapters.ShellStatus(cmd.Context(), db, sh)
		fmt.Fprintf(cmd.OutOrStdout(), "%s Installed %s shell hook into %s\n", styleFor(cmd).green("[OK]"), sh, info.InstallPath)
		return nil
	}

	if interactive {
		return runInteractiveHooks(cmd, db, notifyBin, uninstall, shellMinMS)
	}

	if all {
		for _, name := range adapters.Names() {
			if err := installOneAgent(cmd, db, notifyBin, name, uninstall); err != nil {
				return err
			}
		}
		return nil
	}

	if agent == "" {
		return errors.New("--agent, --shell, --all, or --interactive required")
	}
	return installOneAgent(cmd, db, notifyBin, agent, uninstall)
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
	Record            *hookRecordView       `json:"record,omitempty"`
	BackupPath        string                `json:"backup_path,omitempty"`
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

func runHooksShow(cmd *cobra.Command, _ []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	all, _ := cmd.Flags().GetBool("all")
	asJSON, _ := cmd.Flags().GetBool("json")
	if agent == "" && !all {
		return errors.New("--agent or --all required")
	}
	db, err := openDefaultDB()
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

func buildHooksShowReport(cmd *cobra.Command, db *store.DB, name, notifyBin string) (hooksShowReport, error) {
	a, ok := adapters.Get(name)
	if !ok {
		return hooksShowReport{}, adapters.Unsupported(name)
	}
	info := a.Status(cmd.Context(), db)
	report := hooksShowReport{
		Agent:      a.Name,
		Support:    info.Support,
		ConfigPath: info.InstallPath,
		Expected:   []common.ExpectedHook{},
		Observed:   []common.ObservedHook{},
		Message:    info.Message,
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

func runInteractiveHooks(cmd *cobra.Command, db *store.DB, notifyBin string, uninstall bool, shellMinMS int64) error {
	br := bufio.NewReader(cmd.InOrStdin())
	for _, name := range adapters.Names() {
		if _, err := exec.LookPath(name); err != nil && name != "copilot" {
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s hooks? [Y/n] ", action(uninstall), name)
		line, _ := br.ReadString('\n')
		if strings.EqualFold(strings.TrimSpace(line), "n") {
			continue
		}
		if err := installOneAgent(cmd, db, notifyBin, name, uninstall); err != nil {
			return err
		}
	}
	for _, sh := range adapters.ShellNames() {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s shell hook? [y/N] ", action(uninstall), sh)
		line, _ := br.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(line), "y") {
			continue
		}
		if uninstall {
			if err := adapters.UninstallShell(cmd.Context(), db, sh); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled %s shell hook\n", styleFor(cmd).green("[OK]"), sh)
		} else if err := adapters.InstallShell(cmd.Context(), db, notifyBin, sh, shellMinMS); err != nil {
			return err
		}
	}
	return nil
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
