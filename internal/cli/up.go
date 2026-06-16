package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/store"
)

var setupRun = runSetup
var installServiceRun = runInstallService

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Set up Onibi if needed, otherwise install service and run doctor",
		RunE:  runUp,
	}
}

func runUp(cmd *cobra.Command, _ []string) error {
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

	ownerSet, err := auth.IsOwnerSet(cmd.Context(), db)
	if err != nil {
		return err
	}
	if !ownerSet {
		fmt.Fprintln(cmd.OutOrStdout(), "Not paired yet - running setup --complete.")
		setup := setupCmd()
		setup.SetContext(cmd.Context())
		setup.SetIn(cmd.InOrStdin())
		setup.SetOut(cmd.OutOrStdout())
		setup.SetErr(cmd.ErrOrStderr())
		if err := setup.Flags().Set("complete", "true"); err != nil {
			return err
		}
		return setupRun(setup, nil)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Already paired - installing service and running doctor.")
	if err := installServiceRun(cmd, nil); err != nil {
		return err
	}
	style := styleFor(cmd)
	report := doctorRun(cmd.Context(), doctor.Options{Paths: paths, Mode: "installed"})
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", style.status(c.Status), c.Name, c.Detail)
	}
	if report.Failed() {
		return fmt.Errorf("doctor failed: see output above")
	}
	return nil
}
