package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/webadmin"
	"github.com/spf13/cobra"
)

func webAdminCall(cmd *cobra.Command, request webadmin.Request) (webadmin.Response, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return webadmin.Response{}, err
	}
	return webadmin.Call(contextOrBackground(cmd.Context()), webadmin.Path(paths.StateDir), request)
}
func webPairCmd() *cobra.Command {
	return &cobra.Command{Use: "pair", Short: "Print a fresh one-use browser link without restarting Onibi", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		response, err := webAdminCall(cmd, webadmin.Request{Operation: "pair"})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "One use · expires in 10 minutes · replaces any unused pairing link\n%s\n", response.PairingURL)
		return nil
	}}
}
func webDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "devices", Short: "List or revoke browsers paired with the running web server"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		response, err := webAdminCall(cmd, webadmin.Request{Operation: "devices"})
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(response.Devices)
		}
		if len(response.Devices) == 0 {
			cmd.Println("No active browsers.")
		}
		for _, d := range response.Devices {
			kind := "temporary"
			if d.Remembered {
				kind = "remembered"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  expires %s\n", d.ID, d.Name, kind, d.ExpiresAt.Format(time.RFC3339))
		}
		return nil
	}}
	list.Flags().Bool("json", false, "print device metadata as JSON")
	revoke := &cobra.Command{Use: "revoke <device-id>", Short: "Revoke one device and all its active browser sessions", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, err := webAdminCall(cmd, webadmin.Request{Operation: "revoke", ID: args[0]})
		if err != nil {
			return err
		}
		cmd.Println("Device access revoked. Already dispatched commands cannot be recalled.")
		return nil
	}}
	cmd.AddCommand(list, revoke)
	return cmd
}
