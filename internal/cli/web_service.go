package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/spf13/cobra"
)

func webServiceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "service", Short: "Manage the Mac web cockpit as a separate login service"}
	for _, op := range []string{"install", "start", "stop", "restart", "status", "remove"} {
		sub := &cobra.Command{Use: op, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := config.DefaultPaths()
			if err != nil {
				return err
			}
			base, err := service.NewManager(paths, "")
			if err != nil {
				return err
			}
			manager := service.WebManager{Manager: base}
			ctx, cancel := context.WithTimeout(contextOrBackground(cmd.Context()), 55*time.Second)
			defer cancel()
			switch op {
			case "install":
				listen, _ := cmd.Flags().GetString("listen")
				origin, _ := cmd.Flags().GetString("origin")
				if err := validateWebListen(listen); err != nil {
					return err
				}
				if err := manager.Install(ctx, listen, origin); err != nil {
					return err
				}
				cmd.Println("Web login service installed. Run onibi web service start, then onibi web pair. It also starts at your next graphical login.")
				return nil
			case "start":
				err = manager.Start(ctx)
			case "stop":
				err = manager.Stop(ctx)
			case "restart":
				err = manager.Restart(ctx)
			case "remove":
				err = manager.Remove(ctx)
			case "status":
				status := manager.Status(ctx)
				asJSON, _ := cmd.Flags().GetBool("json")
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Installed: %t\nRunning: %t\nFile: %s\n%s\n", status.Installed, status.Running, status.Path, status.Detail)
				return nil
			}
			if err == nil {
				cmd.Printf("Web service %s completed.\n", op)
			}
			return err
		}}
		if op == "install" {
			sub.Flags().String("listen", "127.0.0.1:8787", "fixed loopback address")
			sub.Flags().String("origin", "", "private Tailscale HTTPS origin (defaults to loopback HTTP)")
		}
		if op == "status" {
			sub.Flags().Bool("json", false, "print service state as JSON")
		}
		cmd.AddCommand(sub)
	}
	return cmd
}
