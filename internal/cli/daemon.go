package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/courtsg/internal/daemon"
)

func newDaemonCommand(runtime *runtime) *cobra.Command {
	var once bool
	var interval time.Duration
	command := &cobra.Command{
		Use:   "daemon",
		Short: "Run shared refresh, watch evaluation, and delivery retries",
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			if interval <= 0 {
				interval = time.Duration(service.Config().Daemon.RefreshMinutes) * time.Minute
			}
			runner, err := daemon.New(service, service.Config().DataDir, interval)
			if err != nil {
				return err
			}
			if once {
				report, err := runner.RunOnce(command.Context())
				if err != nil {
					return err
				}
				if runtime.json {
					return writeJSON(command.OutOrStdout(), report)
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "refreshed: %d\nwatches: %d\ndeliveries retried: %d\n", len(report.Refreshed), len(report.Watches), len(report.Deliveries))
				return err
			}
			return runner.Run(command.Context())
		},
	}
	command.Flags().BoolVar(&once, "once", false, "run one shared cycle and exit")
	command.Flags().DurationVar(&interval, "interval", 0, "cycle interval (minimum 1m; defaults to daemon.refresh_minutes)")
	return command
}
