package cli

import (
	"encoding/json"
	"os/exec"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/spf13/cobra"
)

type adapterStatus struct {
	Name             string `json:"name"`
	Support          string `json:"support"`
	Detected         bool   `json:"detected"`
	Installed        bool   `json:"installed"`
	Managed          bool   `json:"managed"`
	HashRecorded     bool   `json:"hash_recorded"`
	Tampered         bool   `json:"tampered"`
	Adoptable        bool   `json:"adoptable"`
	InstalledVersion string `json:"installed_version,omitempty"`
	BundledVersion   string `json:"bundled_version"`
	Outdated         bool   `json:"outdated"`
	Path             string `json:"path,omitempty"`
	Message          string `json:"message,omitempty"`
	Next             string `json:"next,omitempty"`
}

func runAdapters(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	var rows []adapterStatus
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(cmd.Context(), db)
		rows = append(rows, statusFromInfo(info, agentDetected(name)))
	}
	for _, sh := range adapters.ShellNames() {
		info := adapters.ShellStatus(cmd.Context(), db, sh)
		rows = append(rows, statusFromInfo(info, shellDetected(sh)))
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "NAME", "SUPPORT", "DETECTED", "INSTALLED", "VERSION", "PATH", "STATUS")}
	for _, r := range rows {
		ver := r.InstalledVersion
		if ver == "" {
			ver = "-"
		}
		if r.Outdated {
			ver = style.yellow(ver + " (outdated)")
		}
		table = append(table, []string{
			r.Name,
			r.Support,
			style.bool(r.Detected),
			style.installed(r.Installed),
			ver,
			r.Path,
			adapterMessage(style, r),
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func statusFromInfo(info common.Info, detected bool) adapterStatus {
	installed := ""
	if info.InstalledVersion != nil {
		installed = *info.InstalledVersion
	}
	return adapterStatus{
		Name:             info.Name,
		Support:          info.Support,
		Detected:         detected,
		Installed:        info.Installed,
		Managed:          info.Managed,
		HashRecorded:     info.HashRecorded,
		Tampered:         info.Tampered,
		Adoptable:        info.Adoptable,
		InstalledVersion: installed,
		BundledVersion:   info.BundledVersion,
		Outdated:         info.Outdated,
		Path:             info.InstallPath,
		Message:          info.Message,
		Next:             info.Next,
	}
}

func agentDetected(name string) bool {
	bin := map[string]string{
		"amp":      "amp",
		"claude":   "claude",
		"codex":    "codex",
		"copilot":  "copilot",
		"gemini":   "gemini",
		"goose":    "goose",
		"opencode": "opencode",
		"pi":       "pi",
	}[name]
	if bin == "" {
		return false
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func shellDetected(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func adapterMessage(style cliStyle, r adapterStatus) string {
	if r.Tampered {
		return style.red(r.Message)
	}
	if r.Installed && !r.Outdated {
		return style.green(r.Message)
	}
	if r.Installed && r.Outdated {
		return style.yellow(r.Message)
	}
	if r.Next != "" {
		return style.yellow(r.Message)
	}
	return style.dim(r.Message)
}
