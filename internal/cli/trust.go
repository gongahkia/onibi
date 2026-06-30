package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/trust"
)

var trustRPCRequest = func(ctx context.Context, ev intake.Event) (intake.Response, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return intake.Response{}, err
	}
	resp, err := intake.Request(paths.Socket, ev, 30*time.Second)
	if err != nil {
		return intake.Response{}, fmt.Errorf("onibi service unavailable; run `onibi up`: %w", err)
	}
	if strings.TrimSpace(resp.Reason) != "" {
		return resp, errors.New(resp.Reason)
	}
	return resp, nil
}

func trustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "List and mutate trust rules",
		RunE:  runTrustList,
	}
	cmd.PersistentFlags().String("root", "", "project root; default current directory")
	list := &cobra.Command{
		Use:   "list",
		Short: "List trust rules",
		Args:  cobra.ExactArgs(0),
		RunE:  runTrustList,
	}
	list.Flags().Bool("json", false, "print JSON")
	add := &cobra.Command{
		Use:   "add",
		Short: "Add an ephemeral runtime trust rule",
		Args:  cobra.ExactArgs(0),
		RunE:  runTrustAdd,
	}
	add.Flags().String("tool", "", "tool glob, e.g. Edit")
	add.Flags().String("path", "", "path glob relative to root, e.g. src/**")
	add.Flags().String("agent", "", "optional agent name")
	add.Flags().String("effect", string(trust.EffectAutoApprove), "effect: auto_approve|always_prompt|deny")
	add.Flags().String("expires", "5m", "runtime rule duration")
	remove := &cobra.Command{
		Use:     "remove <rule-id>",
		Aliases: []string{"rm"},
		Short:   "Remove a runtime or file trust rule",
		Args:    cobra.ExactArgs(1),
		RunE:    runTrustRemove,
	}
	reload := &cobra.Command{
		Use:   "reload",
		Short: "Reload .onibi/trust.toml",
		Args:  cobra.ExactArgs(0),
		RunE:  runTrustReload,
	}
	persist := &cobra.Command{
		Use:   "persist",
		Short: "Persist runtime trust rules into .onibi/trust.toml",
		Args:  cobra.ExactArgs(0),
		RunE:  runTrustPersist,
	}
	cmd.AddCommand(list, add, remove, reload, persist)
	return cmd
}

func runTrustList(cmd *cobra.Command, _ []string) error {
	root, err := trustRoot(cmd)
	if err != nil {
		return err
	}
	resp, err := trustRPCRequest(cmd.Context(), intake.Event{Type: intake.TypeTrust, TrustAction: "list", TrustRoot: root})
	if err != nil {
		return err
	}
	var view trust.View
	if err := json.Unmarshal([]byte(resp.Text), &view); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	return renderTrustView(cmd, view)
}

func runTrustAdd(cmd *cobra.Command, _ []string) error {
	root, err := trustRoot(cmd)
	if err != nil {
		return err
	}
	tool, _ := cmd.Flags().GetString("tool")
	path, _ := cmd.Flags().GetString("path")
	agent, _ := cmd.Flags().GetString("agent")
	effect, _ := cmd.Flags().GetString("effect")
	expires, _ := cmd.Flags().GetString("expires")
	if strings.TrimSpace(tool) == "" {
		return errors.New("--tool required")
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("--path required")
	}
	resp, err := trustRPCRequest(cmd.Context(), intake.Event{
		Type:        intake.TypeTrust,
		TrustAction: "add",
		TrustRoot:   root,
		Tool:        tool,
		FilePath:    path,
		Agent:       agent,
		Effect:      effect,
		Expires:     expires,
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func runTrustRemove(cmd *cobra.Command, args []string) error {
	return runTrustMutation(cmd, "remove", args[0])
}

func runTrustReload(cmd *cobra.Command, _ []string) error {
	return runTrustMutation(cmd, "reload", "")
}

func runTrustPersist(cmd *cobra.Command, _ []string) error {
	return runTrustMutation(cmd, "persist", "")
}

func runTrustMutation(cmd *cobra.Command, action, id string) error {
	root, err := trustRoot(cmd)
	if err != nil {
		return err
	}
	resp, err := trustRPCRequest(cmd.Context(), intake.Event{
		Type:        intake.TypeTrust,
		TrustAction: action,
		TrustRoot:   root,
		TrustRuleID: id,
	})
	if err != nil {
		return err
	}
	cmd.Println(resp.Text)
	return nil
}

func renderTrustView(cmd *cobra.Command, view trust.View) error {
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "ID", "SOURCE", "EFFECT", "EXPIRES", "MATCH")}
	for _, root := range view.Roots {
		for _, rule := range root.Rules {
			table = append(table, []string{
				rule.ID,
				rule.Source,
				string(rule.Effect),
				trustExpires(rule),
				trustRuleMatch(rule),
			})
		}
	}
	if len(table) == 1 {
		fmt.Fprintln(cmd.OutOrStdout(), style.dim("trust policy empty"))
		return nil
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func trustRoot(cmd *cobra.Command) (string, error) {
	root := ""
	if flag := cmd.Flag("root"); flag != nil {
		root = flag.Value.String()
	}
	if strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = cwd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func trustRuleMatch(rule trust.RuleView) string {
	var parts []string
	if rule.Tool != "" {
		parts = append(parts, "tool="+rule.Tool)
	}
	if rule.Path != "" {
		parts = append(parts, "path="+rule.Path)
	}
	if rule.Agent != "" {
		parts = append(parts, "agent="+rule.Agent)
	}
	return strings.Join(parts, " ")
}

func trustExpires(rule trust.RuleView) string {
	if rule.ExpiresAt == "" {
		return rule.Expires
	}
	return rule.Expires + " until " + rule.ExpiresAt
}
