package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gongahkia/onibi/internal/pi"
	"github.com/spf13/cobra"
)

func piCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pi", Short: "Install the experimental Pi approval extension"}
	install := &cobra.Command{Use: "install", Short: "Install experimental Pi tool-approval extension", RunE: runPiInstall}
	install.Flags().String("notify", "", "absolute path to onibi-notify")
	install.Flags().Bool("experimental", false, "acknowledge Pi live-event validation is pending")
	status := &cobra.Command{Use: "status", Short: "Show Pi extension path", RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := pi.ExtensionPath()
		if err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), path)
		}
		return err
	}}
	cmd.AddCommand(install, status)
	return cmd
}
func runPiInstall(cmd *cobra.Command, _ []string) error {
	experimental, _ := cmd.Flags().GetBool("experimental")
	if !experimental {
		return fmt.Errorf("Pi integration is experimental; rerun with --experimental")
	}
	notify, _ := cmd.Flags().GetString("notify")
	if notify == "" {
		var err error
		notify, err = defaultNotifyPath()
		if err != nil {
			return err
		}
	}
	path, err := pi.Install(cmd.Context(), notify)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Installed %s\nRun /reload in Pi.\n", path)
	return nil
}
func defaultNotifyPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "onibi-notify")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	path, err := exec.LookPath("onibi-notify")
	if err != nil {
		return "", fmt.Errorf("onibi-notify not found; pass --notify /absolute/path")
	}
	return filepath.Abs(path)
}
