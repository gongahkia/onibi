package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"text/tabwriter"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/spf13/cobra"
)

type adapterStatus struct {
	Name             string `json:"name"`
	Support          string `json:"support"`
	Detected         bool   `json:"detected"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
	BundledVersion   string `json:"bundled_version"`
	Outdated         bool   `json:"outdated"`
	Path             string `json:"path,omitempty"`
	Message          string `json:"message,omitempty"`
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
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSUPPORT\tDETECTED\tINSTALLED\tVERSION\tPATH\tSTATUS")
	for _, r := range rows {
		ver := r.InstalledVersion
		if ver == "" {
			ver = "-"
		}
		if r.Outdated {
			ver += " (outdated)"
		}
		fmt.Fprintf(w, "%s\t%s\t%t\t%t\t%s\t%s\t%s\n", r.Name, r.Support, r.Detected, r.Installed, ver, r.Path, r.Message)
	}
	return w.Flush()
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
		InstalledVersion: installed,
		BundledVersion:   info.BundledVersion,
		Outdated:         info.Outdated,
		Path:             info.InstallPath,
		Message:          info.Message,
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
