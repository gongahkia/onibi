package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/gongahkia/onibi/internal/tmux"
	"github.com/spf13/cobra"
)

func runSessions(cmd *cobra.Command, _ []string) error {
	all, _ := cmd.Flags().GetBool("all")
	n, _ := cmd.Flags().GetInt("n")
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.SessionsRecent(cmd.Context(), n, all)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if all {
			cmd.Println("no sessions")
		} else {
			cmd.Println("no active sessions")
		}
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tAGENT\tMODE\tTRANSPORT\tSTARTED\tSTATE\tCOMMAND\tCWD")
	ctrl := tmux.New()
	for _, s := range rows {
		state := "active"
		if s.Ended {
			state = "ended " + ago(s.EndedAt)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.ID, s.Name, s.Agent, sessionMode(ctrl, s.Transport, s.TmuxTarget, s.Ended), s.Transport, ago(s.StartedAt), state, s.Command, s.CWD)
	}
	return w.Flush()
}

type attachCounter interface {
	AttachCount(ctx context.Context, target string) (int, error)
}

func sessionMode(ctrl attachCounter, transport, target string, ended bool) string {
	if ended {
		return "ended"
	}
	if transport != "tmux" || target == "" {
		return "legacy pty"
	}
	n, err := ctrl.AttachCount(context.Background(), target)
	if err != nil || n == 0 {
		return "headless"
	}
	if n == 1 {
		return "visible"
	}
	return fmt.Sprintf("visible x%d", n)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return time.Since(t).Truncate(time.Second).String() + " ago"
}
