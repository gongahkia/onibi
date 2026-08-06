package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/spf13/cobra"
)

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Create and inspect managed sessions"}
	newCmd := &cobra.Command{Use: "new <shell|codex|pi> [args...]", Short: "Create a persistent session", Args: cobra.MinimumNArgs(1), RunE: runSessionNew}
	newCmd.Flags().String("name", "", "session name")
	newCmd.Flags().String("cwd", "", "working directory")
	list := &cobra.Command{Use: "list", Short: "List active and recent sessions", RunE: runSessionList}
	list.Flags().Bool("all", false, "include ended sessions")
	cmd.AddCommand(newCmd, list)
	return cmd
}
func runSessionNew(cmd *cobra.Command, args []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	name, _ := cmd.Flags().GetString("name")
	cwd, _ := cmd.Flags().GetString("cwd")
	response, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypeSessionNew, Agent: strings.ToLower(args[0]), Args: args[1:], Name: name, CWD: cwd}, 5*time.Second)
	if err != nil {
		return fmt.Errorf("daemon unavailable: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", response.Text, response.SessionID)
	return nil
}
func runSessionList(cmd *cobra.Command, _ []string) error {
	_, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	all, _ := cmd.Flags().GetBool("all")
	rows, err := db.SessionsRecent(cmd.Context(), 50, all)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No sessions.")
		return nil
	}
	for _, s := range rows {
		state := "active"
		if s.Ended {
			state = "ended"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", s.ID[:min(8, len(s.ID))], s.Name, s.Agent, state)
	}
	return nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
