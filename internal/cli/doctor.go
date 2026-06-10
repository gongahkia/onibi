package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
)

func runDoctor(cmd *cobra.Command, _ []string) error {
	offline, _ := cmd.Flags().GetBool("offline")
	mode, _ := cmd.Flags().GetString("mode")
	fix, _ := cmd.Flags().GetBool("fix")
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	opts := doctor.Options{Paths: paths, Offline: offline, Mode: mode}
	if fix {
		fixes := doctor.Fix(ctx, opts)
		for _, a := range fixes.Actions {
			fmt.Fprintf(cmd.OutOrStdout(), "[FIX] %s\n", a)
		}
		for _, err := range fixes.Errors {
			fmt.Fprintf(cmd.OutOrStdout(), "[FIX-FAIL] %v\n", err)
		}
		if fixes.Failed() {
			return fmt.Errorf("doctor fix failed")
		}
	}
	report := doctor.Run(ctx, opts)
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
		if c.Next != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "       next: %s\n", c.Next)
		}
	}
	if report.Failed() {
		return fmt.Errorf("doctor failed")
	}
	return nil
}
