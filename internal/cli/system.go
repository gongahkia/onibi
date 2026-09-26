package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/spf13/cobra"
)

func systemCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "system", Short: "Inspect local Onibi state"}
	status := &cobra.Command{Use: "status", Short: "Show daemon and Telegram state", RunE: runSystemStatus}
	configCmd := &cobra.Command{Use: "config", Short: "Read or write configuration"}
	configCmd.AddCommand(&cobra.Command{Use: "get <key>", Args: cobra.ExactArgs(1), RunE: runConfigGet}, &cobra.Command{Use: "set <key> <value>", Args: cobra.ExactArgs(2), RunE: runConfigSet}, &cobra.Command{Use: "list", RunE: runConfigList})
	serviceCmd := &cobra.Command{Use: "service", Short: "Manage background Onibi service"}
	serviceCmd.AddCommand(&cobra.Command{Use: "install", RunE: runServiceInstall}, &cobra.Command{Use: "remove", RunE: runServiceRemove}, &cobra.Command{Use: "status", RunE: runServiceStatus})
	logs := &cobra.Command{Use: "logs", Short: "Show recent daemon logs", RunE: runSystemLogs}
	logs.Flags().Int("tail", 100, "number of lines")
	serviceCmd.AddCommand(&cobra.Command{Use: "restart", RunE: runServiceRestart})
	cmd.AddCommand(status, configCmd, serviceCmd, logs)
	return cmd
}
func runSystemLogs(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	path := filepath.Join(paths.LogDir, "onibi.log")
	lines, _ := cmd.Flags().GetInt("tail")
	if lines < 1 || lines > 5000 {
		return fmt.Errorf("--tail must be between 1 and 5000")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(parts, "\n"))
	return err
}
func runServiceRestart(cmd *cobra.Command, _ []string) error {
	m, err := manager()
	if err != nil {
		return err
	}
	return m.Restart(cmd.Context())
}

func runSystemStatus(cmd *cobra.Command, _ []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.SessionsActive(cmd.Context())
	if err != nil {
		return err
	}
	ping, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypePing}, 750*time.Millisecond)
	daemonRunning := err == nil
	m, managerErr := manager()
	serviceRunning := false
	serviceInstalled := false
	if managerErr == nil {
		status := m.Status(cmd.Context())
		serviceRunning, serviceInstalled = status.Running, status.Installed
	}
	fmt.Fprintf(cmd.OutOrStdout(), "state=%s\nsessions=%d\ndaemon_running=%t\nservice_installed=%t\nservice_running=%t\n", paths.StateDir, len(rows), daemonRunning, serviceInstalled, serviceRunning)
	if daemonRunning {
		fmt.Fprintf(cmd.OutOrStdout(), "daemon_status=%s\n", strings.ReplaceAll(strings.TrimSpace(ping.Text), "\n", "; "))
	}
	return runTelegramStatus(cmd, nil)
}
func loadConfig() (config.Paths, config.Config, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	cfg, _, err := config.Load(paths)
	return paths, cfg, err
}
func runConfigGet(cmd *cobra.Command, args []string) error {
	_, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	v, err := config.Get(cfg, args[0])
	if err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), v)
	}
	return err
}
func runConfigSet(cmd *cobra.Command, args []string) error {
	paths, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, args[0], args[1]); err != nil {
		return err
	}
	if strings.HasPrefix(args[0], "screen.") {
		if err := render.ValidateFont(cfg.Screen.Font, cfg.Screen.FontPath); err != nil {
			return fmt.Errorf("screen font: %w", err)
		}
	}
	return config.Save(paths.Config, cfg)
}
func runConfigList(cmd *cobra.Command, _ []string) error {
	paths, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	_, meta, err := config.Load(paths)
	if err != nil {
		return err
	}
	for _, key := range config.Keys(cfg, meta) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", key.Key, key.Current)
	}
	return nil
}
func manager() (*service.Manager, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return service.NewManager(paths, "")
}
func runServiceInstall(cmd *cobra.Command, _ []string) error {
	m, err := manager()
	if err != nil {
		return err
	}
	if err := m.Install(cmd.Context()); err != nil {
		return err
	}
	path, _ := m.ServicePath()
	fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}
func runServiceRemove(cmd *cobra.Command, _ []string) error {
	m, err := manager()
	if err != nil {
		return err
	}
	return m.Uninstall(cmd.Context())
}
func runServiceStatus(cmd *cobra.Command, _ []string) error {
	m, err := manager()
	if err != nil {
		return err
	}
	s := m.Status(cmd.Context())
	fmt.Fprintf(cmd.OutOrStdout(), "installed=%t\nrunning=%t\n%s\n", s.Installed, s.Running, strings.TrimSpace(s.Detail))
	return nil
}
