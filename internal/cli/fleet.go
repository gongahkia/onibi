package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	fleettransport "github.com/gongahkia/onibi/internal/transport/fleet"
)

func fleetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "fleet", Short: "Validate fleet enrollment endpoints"}
	endpoint := &cobra.Command{
		Use:   "endpoint <mesh|ssh|relay> <address>",
		Short: "Select a fleet enrollment endpoint",
		Args:  cobra.ExactArgs(2),
		RunE:  runFleetEndpoint,
	}
	endpoint.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(endpoint)
	return cmd
}

func runFleetEndpoint(cmd *cobra.Command, args []string) error {
	plan, err := fleettransport.NewEnrollmentPlan(args[0], args[1], time.Now().UTC())
	if err != nil {
		return err
	}
	endpoint, err := plan.Resolve(time.Now().UTC())
	if err != nil {
		return err
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Version  uint16 `json:"version"`
			Adapter  string `json:"adapter"`
			Endpoint any    `json:"endpoint"`
			Expires  string `json:"expires_at"`
		}{Version: plan.Version, Adapter: string(plan.Adapter), Endpoint: endpoint, Expires: plan.ExpiresAt.Format(time.RFC3339)})
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "adapter: %s\nendpoint: %s\n", plan.Adapter, endpoint.URL)
	return err
}
