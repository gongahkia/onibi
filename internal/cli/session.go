package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/spf13/cobra"
)

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Create and inspect managed sessions"}
	newCmd := &cobra.Command{Use: "new <shell|codex|pi|claude> [args...]", Short: "Create a persistent session in tmux, Zellij or Screen", Args: cobra.MinimumNArgs(1), RunE: runSessionNew}
	newCmd.Flags().String("name", "", "session name")
	newCmd.Flags().String("cwd", "", "working directory")
	newCmd.Flags().String("mux", "", "terminal multiplexer (auto, tmux, zellij, screen)")
	list := &cobra.Command{Use: "list", Short: "List active and recent sessions", RunE: runSessionList}
	list.Flags().Bool("all", false, "include ended sessions")
	adopt := &cobra.Command{Use: "adopt <tmux|zellij|screen> <target>", Short: "Control an existing multiplexer session", Args: cobra.ExactArgs(2), RunE: runSessionAdopt}
	adopt.Flags().String("name", "", "session name")
	adopt.Flags().String("cwd", "", "working directory used for mux configuration")
	cmd.AddCommand(newCmd, adopt, list)
	return cmd
}
func runSessionAdopt(cmd *cobra.Command, args []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	name, _ := cmd.Flags().GetString("name")
	cwd, _ := cmd.Flags().GetString("cwd")
	response, err := sessionRequest(paths.Socket, intake.Event{Type: intake.TypeSessionAdopt, Name: name, CWD: cwd, Mux: strings.ToLower(args[0]), Text: args[1]})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", response.Text, response.SessionID)
	return nil
}
func runSessionNew(cmd *cobra.Command, args []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	name, _ := cmd.Flags().GetString("name")
	cwd, _ := cmd.Flags().GetString("cwd")
	mux, _ := cmd.Flags().GetString("mux")
	response, err := sessionRequest(paths.Socket, intake.Event{Type: intake.TypeSessionNew, Agent: strings.ToLower(args[0]), Args: args[1:], Name: name, CWD: cwd, Mux: mux})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", response.Text, response.SessionID)
	return nil
}

// sessionRequest separates an unreachable daemon from one that refused the
// request; the latter reports its reason in an otherwise successful reply.
func sessionRequest(socket string, ev intake.Event) (intake.Response, error) {
	response, err := intake.Request(socket, ev, 5*time.Second)
	if err != nil {
		return intake.Response{}, fmt.Errorf("daemon unavailable: %w", err)
	}
	if response.Reason != "" {
		return intake.Response{}, errors.New(response.Reason)
	}
	return response, nil
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
