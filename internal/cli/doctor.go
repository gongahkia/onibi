package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
)

func runDoctor(cmd *cobra.Command, _ []string) error {
	offline, _ := cmd.Flags().GetBool("offline")
	mode, _ := cmd.Flags().GetString("mode")
	fix, _ := cmd.Flags().GetBool("fix")
	afterUpgrade, _ := cmd.Flags().GetBool("after-upgrade")
	asJSON, _ := cmd.Flags().GetBool("json")
	explain, _ := cmd.Flags().GetBool("explain")
	if afterUpgrade {
		offline = true
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	opts := doctor.Options{Paths: paths, Offline: offline, Mode: mode, AfterUpgrade: afterUpgrade}
	if doctorOptionsHook != nil {
		doctorOptionsHook(&opts)
	}
	style := styleFor(cmd)
	if fix {
		fixes := doctor.Fix(ctx, opts)
		for _, a := range fixes.Actions {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.fix(true), a)
		}
		for _, err := range fixes.Errors {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %v\n", style.fix(false), err)
		}
		if fixes.Failed() {
			return fmt.Errorf("doctor fix failed")
		}
	}
	report := doctor.Run(ctx, opts)
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		if report.Failed() {
			return fmt.Errorf("doctor failed")
		}
		return nil
	}
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s", style.status(c.Status), c.Name, c.Detail)
		if c.Next != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " %s=%s", style.dim("next"), c.Next)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		if explain && c.Status != doctor.Pass {
			printExplainLine(cmd, "impact", c.Impact)
			printExplainLine(cmd, "safe fix", c.SafeFix)
			printExplainLine(cmd, "manual fix", c.ManualFix)
			printExplainLine(cmd, "files", strings.Join(c.FilesTouched, ", "))
			printExplainLine(cmd, "retry", c.Retry)
			printExplainLine(cmd, "blocks", strings.Join(c.Blocks, ", "))
		}
	}
	if report.Failed() {
		return fmt.Errorf("doctor failed")
	}
	return nil
}

func printExplainLine(cmd *cobra.Command, label, value string) {
	if value == "" {
		value = "-"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "       %s: %s\n", styleFor(cmd).dim(label), value)
}

var doctorOptionsHook func(*doctor.Options)
