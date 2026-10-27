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
	cmd := &cobra.Command{Use: "service", Short: "Run the web cockpit and sessions daemon as one login service"}
	shorts := map[string]string{
		"install": "Install the login service (it includes the sessions daemon)",
		"start":   "Start the login service now",
		"stop":    "Stop the login service until the next login",
		"restart": "Restart the login service",
		"status":  "Show whether the login service is installed and running",
		"remove":  "Remove the login service; browser grants and logs are kept",
	}
	for _, op := range []string{"install", "start", "stop", "restart", "status", "remove"} {
		sub := &cobra.Command{Use: op, Short: shorts[op], Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
				if err := terminalUI(cmd).Step("Install web login service", func(func(string)) error { return manager.Install(ctx, listen, origin) }); err != nil {
					return err
				}
				cmd.Printf("Installed. Start: %s web service start\nThen pair: %s web pair\nIt also starts at your next graphical login and runs the sessions daemon, so %s system service is not needed.\n", commandHint(), commandHint(), commandHint())
				return nil
			case "start":
				err = terminalUI(cmd).Step("Start web login service", func(func(string)) error { return manager.Start(ctx) })
			case "stop":
				err = terminalUI(cmd).Step("Stop web login service", func(func(string)) error { return manager.Stop(ctx) })
			case "restart":
				err = terminalUI(cmd).Step("Restart web login service", func(func(string)) error { return manager.Restart(ctx) })
			case "remove":
				err = terminalUI(cmd).Step("Remove web login service", func(func(string)) error { return manager.Remove(ctx) })
			case "status":
				status := manager.Status(ctx)
				asJSON, _ := cmd.Flags().GetBool("json")
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
				}
				ui := terminalUI(cmd)
				ui.Banner("Web login service")
				ui.Field("Installed", fmt.Sprint(status.Installed))
				ui.Field("Running", fmt.Sprint(status.Running))
				ui.Field("File", status.Path)
				ui.Note(status.Detail)
				return nil
			}
			if err == nil {
				switch op {
				case "start", "restart":
					cmd.Printf("Inspect: %s web status\nPair: %s web pair\n", commandHint(), commandHint())
				case "remove":
					cmd.Println("Login service removed. Browser grants, state and logs were preserved.")
				case "stop":
					cmd.Println("Stopped. The installed service starts again at your next graphical login.")
				}
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
