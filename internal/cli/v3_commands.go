package cli

import "github.com/spf13/cobra"

func startCmd() *cobra.Command {
	cmd := upCmd()
	cmd.Use = "start"
	cmd.Short = "Start your Onibi cockpit"
	cmd.Long = "Start a private coding-agent cockpit and pair a phone. On a fresh install, Onibi guides setup before starting."
	return cmd
}

func phoneCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "phone", Short: "Pair and manage your phone"}
	pair := pairCmd()
	pair.Use = "pair"
	pair.Short = "Show a fresh phone pairing QR"
	list := devicesCmd()
	list.Use = "list"
	list.Short = "List paired phones"
	remove := unpairCmd()
	remove.Use = "remove <device-id>"
	remove.Short = "Remove a paired phone"
	cmd.AddCommand(pair, list, remove)
	return cmd
}

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Create and control coding sessions"}
	create := newSessionCmd()
	create.Use = "create [--headless|--visible] <agent|shell> [args...]"
	create.Short = "Create a managed session"
	list := sessionsCmd()
	list.Use = "list"
	list.Short = "List managed sessions"
	cmd.AddCommand(runCmd(), wrapCmd(), shellCmd(), create, list, showCmd(), hideCmd(), sessionSnapshotCmd())
	return cmd
}

func sessionSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot", Short: "Save, restore, and fork session snapshots"}
	create := &cobra.Command{Use: "create <session> <name>", Short: "Save a snapshot", Args: cobra.ExactArgs(2), RunE: runSnapshotTake}
	restore := &cobra.Command{Use: "restore <name>", Short: "Restore a snapshot", Args: cobra.ExactArgs(1), RunE: runSnapshotRestore}
	fork := &cobra.Command{Use: `fork <name> @turn-N "new prompt"`, Short: "Fork a snapshot from a transcript turn", Args: cobra.ExactArgs(3), RunE: runSnapshotFork}
	list := &cobra.Command{Use: "list", Short: "List saved snapshots", RunE: runSnapshotsList}
	remove := &cobra.Command{Use: "remove <name>", Short: "Delete a snapshot", Args: cobra.ExactArgs(1), RunE: runSnapshotDelete}
	cmd.AddCommand(create, restore, fork, list, remove)
	return cmd
}

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Connect and inspect coding agents"}
	install := installHooksCmd()
	install.Use = "install"
	install.Short = "Install Onibi agent hooks"
	status := &cobra.Command{Use: "status", Short: "Show agent and shell adapter status", RunE: runAdapters}
	status.Flags().Bool("json", false, "print JSON")
	inspect := &cobra.Command{Use: "inspect", Short: "Inspect installed hooks and drift", RunE: runHooksShow}
	inspect.Flags().String("agent", "", "agent name")
	inspect.Flags().Bool("all", false, "show every supported agent")
	inspect.Flags().Bool("json", false, "print JSON")
	matrix := &cobra.Command{Use: "matrix", Short: "Show hook compatibility", RunE: runHooksMatrix}
	matrix.Flags().Bool("json", false, "print JSON")
	adapter := &cobra.Command{Use: "adapter", Short: "Manage third-party adapter manifests"}
	adapter.AddCommand(adaptersAddCmd(), adaptersValidateCmd())
	cmd.AddCommand(install, status, inspect, matrix, adapter)
	return cmd
}

func transportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "transport", Short: "Configure reachability and HTTPS transport"}
	expose := tunnelCmd()
	expose.Use = "expose <port>"
	expose.Short = "Expose a local HTTPS port"
	ngrok := ngrokCmd()
	cmd.AddCommand(expose, ngrok)
	return cmd
}

func systemCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "system", Short: "Inspect, configure, and maintain Onibi"}
	service := &cobra.Command{Use: "service", Short: "Manage the background service"}
	install := installServiceCmd()
	install.Use = "install"
	remove := uninstallServiceCmd()
	remove.Use = "remove"
	service.AddCommand(install, remove)
	logs := &cobra.Command{Use: "logs", Short: "Read daemon and audit logs"}
	show := logCmd()
	show.Use = "show"
	follow := tailLogCmd()
	follow.Use = "follow"
	logs.AddCommand(show, follow)
	data := &cobra.Command{Use: "data", Short: "Maintain encrypted local data"}
	data.AddCommand(storeCmd().Commands()[0])
	support := &cobra.Command{Use: "support", Short: "Create support diagnostics"}
	bundle := supportBundleCmd()
	bundle.Use = "bundle"
	support.AddCommand(bundle)
	cmd.AddCommand(statusCmd(), doctorCmd(), pingCmd(), configCmd(), service, logs, data, pushCmd(), support, uninstallCmd(), logoCmd())
	return cmd
}
