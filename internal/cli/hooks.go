package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
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
			fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s shell hook\n", sh)
			return nil
		}
		if err := adapters.InstallShell(cmd.Context(), db, notifyBin, sh, shellMinMS); err != nil {
			return err
		}
		info := adapters.ShellStatus(cmd.Context(), db, sh)
		fmt.Fprintf(cmd.OutOrStdout(), "Installed %s shell hook into %s\n", sh, info.InstallPath)
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
		fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s hooks\n", a.Name)
		return nil
	}
	if err := a.Install(cmd.Context(), db, notifyBin); err != nil {
		return err
	}
	info := a.Status(cmd.Context(), db)
	fmt.Fprintf(cmd.OutOrStdout(), "Installed %s hooks into %s\n", a.Name, info.InstallPath)
	return nil
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
			fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s shell hook\n", sh)
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
func locateNotifyBinary() (string, error) {
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
