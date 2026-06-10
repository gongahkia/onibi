package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters/claude"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

// runInstallHooks implements `onibi install-hooks --agent <name>`.
// Phase 2: Claude only. Phase 8 expands.
func runInstallHooks(cmd *cobra.Command, _ []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	if agent == "" {
		return errors.New("--agent required (claude is currently the only supported agent)")
	}

	notifyBin, err := locateNotifyBinary()
	if err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()

	switch agent {
	case "claude":
		if err := claude.Install(cmd.Context(), db, notifyBin); err != nil {
			return err
		}
		path, _ := claude.SettingsPath()
		fmt.Fprintf(cmd.OutOrStdout(), "Installed Claude Stop hook into %s\n", path)
		fmt.Fprintf(cmd.OutOrStdout(), "Hook calls: %s --type agent_done\n", notifyBin)
		return nil
	default:
		return fmt.Errorf("agent %q not supported yet (phase 8 adds codex, opencode, goose)", agent)
	}
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
