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
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	report := doctor.Run(ctx, doctor.Options{Paths: paths, Offline: offline})
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
	}
	if report.Failed() {
		return fmt.Errorf("doctor failed")
	}
	return nil
}
