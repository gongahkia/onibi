package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

func uninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Onibi service, hooks, and optionally local state",
		RunE:  runUninstall,
	}
	cmd.Flags().Bool("service", false, "remove LaunchAgent/systemd user unit")
	cmd.Flags().Bool("hooks", false, "remove Onibi-managed hooks")
	cmd.Flags().Bool("all-hooks", false, "remove all supported agent and shell hooks")
	cmd.Flags().String("agent", "", "remove one agent hook")
	cmd.Flags().String("shell", "", "remove one shell hook")
	cmd.Flags().Bool("state", false, "remove local state, logs, sqlite, config, and fallback .env")
	cmd.Flags().Bool("yes", false, "required with --state")
	cmd.Flags().Bool("dry-run", false, "print planned actions only")
	return cmd
}

func runUninstall(cmd *cobra.Command, _ []string) error {
	serviceFlag, _ := cmd.Flags().GetBool("service")
	hooksFlag, _ := cmd.Flags().GetBool("hooks")
	allHooks, _ := cmd.Flags().GetBool("all-hooks")
	agent, _ := cmd.Flags().GetString("agent")
	sh, _ := cmd.Flags().GetString("shell")
	stateFlag, _ := cmd.Flags().GetBool("state")
	yes, _ := cmd.Flags().GetBool("yes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if stateFlag && !yes {
		return errors.New("--state requires --yes")
	}
	if !serviceFlag && !hooksFlag && !allHooks && agent == "" && sh == "" && !stateFlag {
		serviceFlag = true
		hooksFlag = true
		allHooks = true
	}
	if allHooks {
		hooksFlag = true
	}
	if agent != "" || sh != "" {
		hooksFlag = true
	}
	if hooksFlag && agent == "" && sh == "" {
		allHooks = true
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	planUninstall(cmd, paths, serviceFlag, hooksFlag, allHooks, agent, sh, stateFlag)
	if dryRun {
		return nil
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	if serviceFlag {
		m, err := serviceManager()
		if err != nil {
			return err
		}
		if err := m.Uninstall(ctx); err != nil {
			return err
		}
		path, _ := m.ServicePath()
		fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled service %s\n", path)
	}
	if hooksFlag {
		if err := uninstallHooks(ctx, cmd, paths, allHooks, agent, sh); err != nil {
			return err
		}
	}
	if stateFlag {
		if err := deleteSecrets(paths); err != nil {
			return err
		}
		if err := os.RemoveAll(paths.StateDir); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Removed state %s\n", paths.StateDir)
	}
	return nil
}

func planUninstall(cmd *cobra.Command, paths config.Paths, serviceFlag, hooksFlag, allHooks bool, agent, sh string, stateFlag bool) {
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "ACTION", "TARGET")}
	if serviceFlag {
		table = append(table, []string{style.yellow("remove service"), "LaunchAgent/systemd user unit"})
	}
	if hooksFlag {
		if allHooks {
			table = append(table, []string{style.yellow("remove hooks"), "all supported agents and shells"})
		} else {
			if agent != "" {
				table = append(table, []string{style.yellow("remove hook"), "agent:" + agent})
			}
			if sh != "" {
				table = append(table, []string{style.yellow("remove hook"), "shell:" + sh})
			}
		}
	}
	if stateFlag {
		table = append(table, []string{style.red("remove state"), paths.StateDir})
		table = append(table, []string{style.red("remove secrets"), "bot token and TOTP secret from active backend"})
	}
	_ = renderTable(cmd.OutOrStdout(), table)
}

func uninstallHooks(ctx context.Context, cmd *cobra.Command, paths config.Paths, all bool, agent, sh string) error {
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()

	if all {
		for _, name := range adapters.Names() {
			if err := uninstallAgentHook(ctx, cmd, db, name); err != nil {
				return err
			}
		}
		for _, name := range adapters.ShellNames() {
			if err := adapters.UninstallShell(ctx, db, name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s shell hook\n", name)
		}
		return nil
	}
	if agent != "" {
		if err := uninstallAgentHook(ctx, cmd, db, agent); err != nil {
			return err
		}
	}
	if sh != "" {
		if err := adapters.UninstallShell(ctx, db, sh); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s shell hook\n", sh)
	}
	return nil
}

func uninstallAgentHook(ctx context.Context, cmd *cobra.Command, db *store.DB, name string) error {
	a, ok := adapters.Get(name)
	if !ok {
		return adapters.Unsupported(name)
	}
	if err := a.Uninstall(ctx, db); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled %s hooks\n", a.Name)
	return nil
}

func deleteSecrets(paths config.Paths) error {
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	if err := sec.Delete(secrets.KeyBotToken); err != nil {
		return err
	}
	return sec.Delete(secrets.KeyTOTPSecret)
}
