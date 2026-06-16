package cli

import (
	"context"
	"fmt"
	"strings"
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
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "ID", "NAME", "AGENT", "MODE", "TRANSPORT", "STARTED", "STATE", "COMMAND", "CWD")}
	ctrl := tmux.New()
	for _, s := range rows {
		state := "active"
		if s.Ended {
			state = "ended " + ago(s.EndedAt)
		}
		mode := sessionMode(ctrl, s.Transport, s.TmuxTarget, s.Ended)
		table = append(table, []string{
			s.ID,
			s.Name,
			s.Agent,
			styleSessionMode(style, mode),
			s.Transport,
			ago(s.StartedAt),
			styleSessionState(style, state, s.Ended),
			s.Command,
			s.CWD,
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
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

func styleSessionMode(style cliStyle, mode string) string {
	switch {
	case mode == "headless":
		return style.yellow(mode)
	case strings.HasPrefix(mode, "visible"):
		return style.green(mode)
	case mode == "legacy pty":
		return style.dim(mode)
	default:
		return mode
	}
}

func styleSessionState(style cliStyle, state string, ended bool) string {
	if ended {
		return style.dim(state)
	}
	return style.green(state)
}
