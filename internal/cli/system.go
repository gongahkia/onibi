package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/termui"
	"github.com/spf13/cobra"
)

func systemCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "system", Short: "Inspect local Onibi state"}
	status := &cobra.Command{Use: "status", Short: "Show daemon and service state", Args: cobra.NoArgs, RunE: runSystemStatus}
	status.Flags().Bool("json", false, "print machine-readable JSON")
	configCmd := &cobra.Command{Use: "config", Short: "Read or write configuration"}
	resolve := &cobra.Command{Use: "resolve", Short: "Show the multiplexer configuration resolved for a working directory", RunE: runConfigResolve}
	resolve.Flags().String("cwd", "", "project working directory")
	configCmd.AddCommand(&cobra.Command{Use: "get <key>", Short: "Print one configuration value", Args: cobra.ExactArgs(1), ValidArgsFunction: completeConfigKeys, RunE: runConfigGet}, &cobra.Command{Use: "set <key> <value>", Short: "Change one configuration value", Example: "  onibi system config set multiplexer.default zellij", Args: cobra.ExactArgs(2), ValidArgsFunction: completeConfigKeys, RunE: runConfigSet}, &cobra.Command{Use: "list", Short: "List every configuration key and its current value", Args: cobra.NoArgs, RunE: runConfigList}, resolve)
	serviceCmd := &cobra.Command{Use: "service", Short: "Run only the sessions daemon as a login service (onibi web service includes it)"}
	serviceCmd.AddCommand(&cobra.Command{Use: "install", Short: "Install and start the daemon as a login service", Args: cobra.NoArgs, RunE: runServiceInstall}, &cobra.Command{Use: "remove", Short: "Stop and remove the login service", Args: cobra.NoArgs, RunE: runServiceRemove}, &cobra.Command{Use: "status", Short: "Show whether the login service is installed and running", Args: cobra.NoArgs, RunE: runServiceStatus})
	logs := &cobra.Command{Use: "logs", Short: "Show recent daemon logs", RunE: runSystemLogs}
	logs.Flags().Int("tail", 100, "number of lines")
	serviceCmd.AddCommand(&cobra.Command{Use: "restart", Short: "Restart the login service", Args: cobra.NoArgs, RunE: runServiceRestart})
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
	daemonStatus := ""
	if daemonRunning {
		daemonStatus = strings.ReplaceAll(strings.TrimSpace(ping.Text), "\n", "; ")
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"stateDir": paths.StateDir, "sessions": len(rows), "daemonRunning": daemonRunning, "daemonStatus": daemonStatus, "serviceInstalled": serviceInstalled, "serviceRunning": serviceRunning})
	}
	ui := terminalUI(cmd)
	ui.Banner("Sessions daemon / status")
	if daemonRunning {
		ui.Field("Daemon", "Running")
		ui.Field("Status", daemonStatus)
	} else {
		ui.Field("Daemon", "Stopped")
	}
	ui.Field("Sessions", fmt.Sprintf("%d running", len(rows)))
	switch {
	case serviceRunning:
		ui.Field("Service", "Installed and running")
	case serviceInstalled:
		ui.Field("Service", "Installed, not running")
	default:
		ui.Field("Service", "Not installed")
	}
	ui.Field("State", termui.ShortPath(paths.StateDir))
	if !daemonRunning {
		ui.Note("onibi web starts it automatically; onibi start runs it on its own.")
	}
	return nil
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
func runConfigResolve(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cwd, _ := cmd.Flags().GetString("cwd")
	if strings.TrimSpace(cwd) == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	resolved, err := config.ResolveForCWD(paths, cwd)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "global=%s\nglobal_exists=%t\nlocal=%s\nlocal_exists=%t\nmultiplexer.default=%s\nmultiplexer.tmux.bin=%s\nmultiplexer.tmux.config=%s\nmultiplexer.zellij.bin=%s\nmultiplexer.zellij.config=%s\nmultiplexer.screen.bin=%s\nmultiplexer.screen.config=%s\n", resolved.Global.Path, resolved.Global.Exists, resolved.Local.Path, resolved.Local.Exists, resolved.Config.Multiplexer.Default, resolved.Config.Multiplexer.Tmux.Bin, resolved.Config.Multiplexer.Tmux.Config, resolved.Config.Multiplexer.Zellij.Bin, resolved.Config.Multiplexer.Zellij.Config, resolved.Config.Multiplexer.Screen.Bin, resolved.Config.Multiplexer.Screen.Config)
	if path, err := config.ResolveTmuxConfig(resolved.CWD, resolved.Config.Multiplexer.Tmux.Config); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "tmux.config_resolved=%s\n", path)
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

func completeConfigKeys(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	paths, cfg, err := loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	_, meta, _ := config.Load(paths)
	var keys []string
	for _, key := range config.Keys(cfg, meta) {
		keys = append(keys, key.Key)
	}
	return keys, cobra.ShellCompDirectiveNoFileComp
}
