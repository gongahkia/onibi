package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
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
	cmd.Flags().Bool("yes", false, "skip destructive confirmation")
	cmd.Flags().Bool("dry-run", false, "print planned actions only")
	cmd.Flags().Bool("json", false, "print dry-run plan JSON")
	return cmd
}

type uninstallPlanItem struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Risk   string `json:"risk"`
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
	asJSON, _ := cmd.Flags().GetBool("json")

	if asJSON && !dryRun {
		return errors.New("--json requires --dry-run")
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
	plan := buildUninstallPlan(paths, serviceFlag, hooksFlag, allHooks, agent, sh, stateFlag)
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}
	renderUninstallPlan(cmd, plan)
	if dryRun {
		return nil
	}
	if !yes {
		ok, err := confirmUninstall(cmd, stateFlag)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
			return nil
		}
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
		fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled service %s\n", styleFor(cmd).green("[OK]"), path)
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
		fmt.Fprintf(cmd.OutOrStdout(), "%s Removed state %s\n", styleFor(cmd).green("[OK]"), paths.StateDir)
	}
	return nil
}

func buildUninstallPlan(paths config.Paths, serviceFlag, hooksFlag, allHooks bool, agent, sh string, stateFlag bool) []uninstallPlanItem {
	var plan []uninstallPlanItem
	if serviceFlag {
		plan = append(plan, uninstallPlanItem{Action: "remove service", Target: "LaunchAgent/systemd user unit", Risk: "medium"})
	}
	if hooksFlag {
		for _, inspect := range uninstallHookInspectCommands(allHooks, agent, sh) {
			plan = append(plan, uninstallPlanItem{Action: "inspect hooks", Target: inspect, Risk: "low"})
		}
		if allHooks {
			plan = append(plan, uninstallPlanItem{Action: "remove hooks", Target: "all supported agents and shells", Risk: "medium"})
		} else {
			if agent != "" {
				plan = append(plan, uninstallPlanItem{Action: "remove hook", Target: "agent:" + agent, Risk: "medium"})
			}
			if sh != "" {
				plan = append(plan, uninstallPlanItem{Action: "remove hook", Target: "shell:" + sh, Risk: "medium"})
			}
		}
	}
	if stateFlag {
		plan = append(plan,
			uninstallPlanItem{Action: "remove state", Target: paths.StateDir, Risk: "high"},
			uninstallPlanItem{Action: "remove secrets", Target: "Onibi credentials from active backend", Risk: "high"},
		)
	}
	return plan
}

func renderUninstallPlan(cmd *cobra.Command, plan []uninstallPlanItem) {
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "ACTION", "TARGET")}
	for _, item := range plan {
		action := item.Action
		switch item.Risk {
		case "high":
			action = style.red(action)
		case "medium":
			action = style.yellow(action)
		default:
			action = style.dim(action)
		}
		table = append(table, []string{action, item.Target})
	}
	_ = renderTable(cmd.OutOrStdout(), table)
}

func confirmUninstall(cmd *cobra.Command, stateFlag bool) (bool, error) {
	if !inputIsTerminal(cmd.InOrStdin()) || !outputIsTerminal(cmd.OutOrStdout()) {
		return false, errors.New("uninstall requires --yes in non-interactive mode")
	}
	br := bufio.NewReader(cmd.InOrStdin())
	if !stateFlag {
		return askYesNo(cmd, br, "Continue? [y/N] ", false), nil
	}
	const phrase = "delete onibi state"
	fmt.Fprintf(cmd.OutOrStdout(), "Type %q to remove local state and secrets: ", phrase)
	line, err := br.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, err
	}
	return strings.TrimSpace(line) == phrase, nil
}

func uninstallHookInspectCommands(allHooks bool, agent, sh string) []string {
	if allHooks {
		return []string{"onibi hooks --show --all"}
	}
	var out []string
	if agent != "" {
		out = append(out, "onibi hooks --show --agent "+agent)
	}
	if sh != "" {
		out = append(out, "onibi hooks --show --shell "+sh)
	}
	return out
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
			fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled %s shell hook\n", styleFor(cmd).green("[OK]"), name)
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
		fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled %s shell hook\n", styleFor(cmd).green("[OK]"), sh)
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
	fmt.Fprintf(cmd.OutOrStdout(), "%s Uninstalled %s hooks\n", styleFor(cmd).green("[OK]"), a.Name)
	return nil
}

func deleteSecrets(paths config.Paths) error {
	sec, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	return errors.Join(
		sec.Delete(daemon.TelegramSecretBotToken),
		sec.Delete("bot_token"),
		sec.Delete("totp_secret_hex"),
	)
}
