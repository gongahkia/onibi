package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/termui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Start, list, attach to and end persistent sessions"}
	newCmd := &cobra.Command{Use: "new [flags] <shell|program> [args...]", Short: "Run a shell or any program in a persistent tmux, Zellij or Screen session", Example: "  onibi session new shell\n  onibi session new --name review codex --model gpt-5\n  onibi session new htop -d 5", Args: cobra.MinimumNArgs(1), RunE: runSessionNew}
	// Everything after the program belongs to it, not to onibi.
	newCmd.Flags().SetInterspersed(false)
	newCmd.Flags().String("name", "", "session name")
	newCmd.Flags().String("cwd", "", "working directory")
	newCmd.Flags().String("mux", "", "terminal multiplexer (auto, tmux, zellij, screen)")
	newCmd.Flags().Bool("attach", false, "attach this terminal to the session after starting it")
	list := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List sessions with their program, multiplexer and attention state", Args: cobra.NoArgs, RunE: runSessionList}
	list.Flags().Bool("all", false, "include ended sessions")
	list.Flags().Bool("json", false, "print machine-readable JSON")
	adopt := &cobra.Command{Use: "adopt <tmux|zellij|screen> <target>", Short: "Control an existing multiplexer session", Args: cobra.ExactArgs(2), RunE: runSessionAdopt}
	adopt.Flags().String("name", "", "session name")
	adopt.Flags().String("cwd", "", "working directory used for mux configuration")
	attach := &cobra.Command{Use: "attach <name|id>", Aliases: []string{"a"}, Short: "Attach this terminal to a session", Example: "  onibi session attach build\n  onibi session attach 7df5", Args: cobra.ExactArgs(1), ValidArgsFunction: completeSessions, RunE: runSessionAttach}
	kill := &cobra.Command{Use: "kill <name|id>", Short: "End a session and the programs running in it", Args: cobra.ExactArgs(1), ValidArgsFunction: completeSessions, RunE: runSessionKill}
	kill.Flags().BoolP("yes", "y", false, "do not ask for confirmation")
	rename := &cobra.Command{Use: "rename <name|id> <new-name>", Short: "Rename a session (letters, digits, dot, underscore and hyphen)", Example: "  onibi session rename shell-2 review", Args: cobra.ExactArgs(2), ValidArgsFunction: completeSessions, RunE: runSessionRename}
	cmd.AddCommand(newCmd, list, attach, rename, kill, adopt)
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
	fmt.Fprintf(cmd.OutOrStdout(), "Adopted %s (%s)\n", strings.TrimPrefix(response.Text, "adopted "), shortID(response.SessionID))
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
	muxName, _ := cmd.Flags().GetString("mux")
	attach, _ := cmd.Flags().GetBool("attach")
	if cwd == "" {
		// The daemon's working directory is unrelated to the caller's.
		cwd, _ = os.Getwd()
	}
	response, err := sessionRequest(paths.Socket, intake.Event{Type: intake.TypeSessionNew, Agent: args[0], Args: args[1:], Name: name, CWD: cwd, Mux: muxName})
	if err != nil {
		return err
	}
	started := strings.TrimPrefix(response.Text, "started ")
	if attach {
		row, err := resolveSession(cmd, db, response.SessionID)
		if err != nil {
			return err
		}
		return attachSession(row)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Started %s (%s)\n  Attach  onibi session attach %s\n", started, shortID(response.SessionID), started)
	return nil
}

// sessionRequest separates an unreachable daemon from one that refused the
// request; the latter reports its reason in an otherwise successful reply.
func sessionRequest(socket string, ev intake.Event) (intake.Response, error) {
	response, err := intake.Request(socket, ev, 5*time.Second)
	if err != nil {
		return intake.Response{}, fmt.Errorf("daemon unavailable (start it with onibi web or onibi start): %w", err)
	}
	if response.Reason != "" {
		return intake.Response{}, errors.New(response.Reason)
	}
	return response, nil
}

type sessionRow struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Program      string     `json:"program"`
	Multiplexer  string     `json:"multiplexer"`
	CWD          string     `json:"cwd,omitempty"`
	Command      string     `json:"command,omitempty"`
	State        string     `json:"state"`
	Attention    string     `json:"attention,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	LastActivity time.Time  `json:"lastActivityAt"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	target       string
}

func sessionRows(cmd *cobra.Command, db *store.DB, all bool) ([]sessionRow, error) {
	entries, err := db.SessionsRecent(cmd.Context(), 200, all)
	if err != nil {
		return nil, err
	}
	// Attention is only known to a running daemon; the list still works
	// without one.
	attention := map[string]intake.Attention{}
	if paths, err := config.DefaultPaths(); err == nil {
		if resp, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypeSessionList}, 750*time.Millisecond); err == nil {
			for _, s := range resp.Sessions {
				attention[s.ID] = s.Attention
			}
		}
	}
	rows := make([]sessionRow, 0, len(entries))
	for _, e := range entries {
		row := sessionRow{ID: e.ID, Name: e.Name, Program: e.Agent, Multiplexer: e.Transport, CWD: e.CWD, Command: e.Command, State: "running", StartedAt: e.StartedAt, LastActivity: e.LastActivity, target: e.TmuxTarget}
		if e.Ended {
			row.State = "ended"
			ended := e.EndedAt
			row.EndedAt = &ended
		} else if a, ok := attention[e.ID]; ok {
			row.Attention = a.State
			if a.LastOutputAt.After(row.LastActivity) {
				row.LastActivity = a.LastOutputAt
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

var attentionText = map[string]string{"input": "needs input", "bell": "bell", "active": "active", "idle": "idle", "quiet": "quiet"}

func runSessionList(cmd *cobra.Command, _ []string) error {
	_, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	defer db.Close()
	all, _ := cmd.Flags().GetBool("all")
	asJSON, _ := cmd.Flags().GetBool("json")
	rows, err := sessionRows(cmd, db, all)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No sessions. Start one with: onibi session new shell")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tPROGRAM\tMUX\tSTATUS\tLAST ACTIVE\tFOLDER")
	now := time.Now()
	for _, r := range rows {
		status := r.State
		if r.State == "running" && r.Attention != "" {
			status = attentionText[r.Attention]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", termui.Clean(r.Name), shortID(r.ID), termui.Clean(r.Program), r.Multiplexer, status, ago(now, r.LastActivity), termui.ShortPath(r.CWD))
	}
	return w.Flush()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < 10*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// resolveSession accepts a session name, full ID or unique ID prefix of a
// running session.
func resolveSession(cmd *cobra.Command, db *store.DB, query string) (sessionRow, error) {
	query = strings.TrimSpace(query)
	rows, err := sessionRows(cmd, db, false)
	if err != nil {
		return sessionRow{}, err
	}
	var matches []sessionRow
	for _, r := range rows {
		if r.ID == query || r.Name == query {
			return r, nil
		}
		if len(query) >= 4 && strings.HasPrefix(r.ID, query) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return sessionRow{}, fmt.Errorf("no running session named %q; see onibi session list", query)
	default:
		return sessionRow{}, fmt.Errorf("%q matches %d sessions; use more of the ID", query, len(matches))
	}
}

func completeSessions(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	_, db, err := pathsAndStore()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()
	entries, err := db.SessionsRecent(contextOrBackground(cmd.Context()), 200, false)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name+"\t"+e.Agent+" in "+e.Transport)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func runSessionAttach(cmd *cobra.Command, args []string) error {
	_, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	row, err := resolveSession(cmd, db, args[0])
	db.Close()
	if err != nil {
		return err
	}
	return attachSession(row)
}

// attachCommand builds the multiplexer's own attach command, so the session
// behaves exactly as if it had been started by hand.
func attachCommand(row sessionRow, insideTmux bool) (string, []string, error) {
	resolved := config.Default()
	if r, err := config.ResolveForCWD(mustPaths(), row.CWD); err == nil {
		resolved = r.Config
	}
	backend, err := resolved.Multiplexer.Backend(row.Multiplexer)
	if err != nil {
		return "", nil, fmt.Errorf("%s sessions cannot be attached", row.Multiplexer)
	}
	bin := backend.Bin
	if bin == "" {
		bin = row.Multiplexer
	}
	target := mux.ParseTarget(row.target)
	switch row.Multiplexer {
	case "tmux":
		if insideTmux {
			return bin, []string{"switch-client", "-t", "=" + target.Session}, nil
		}
		return bin, []string{"attach-session", "-t", "=" + target.Session}, nil
	case "zellij":
		return bin, []string{"attach", target.Session}, nil
	case "screen":
		return bin, []string{"-r", target.Session}, nil
	}
	return "", nil, fmt.Errorf("%s sessions cannot be attached", row.Multiplexer)
}

func mustPaths() config.Paths {
	paths, _ := config.DefaultPaths()
	return paths
}

func attachSession(row sessionRow) error {
	if !isTerminal(os.Stdin) {
		return errors.New("attach needs an interactive terminal")
	}
	bin, args, err := attachCommand(row, os.Getenv("TMUX") != "" && row.Multiplexer == "tmux")
	if err != nil {
		return err
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", bin)
	}
	return syscall.Exec(path, append([]string{bin}, args...), os.Environ())
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func runSessionKill(cmd *cobra.Command, args []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	row, err := resolveSession(cmd, db, args[0])
	db.Close()
	if err != nil {
		return err
	}
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		if !isTerminal(os.Stdin) {
			return errors.New("refusing to end a session without confirmation; pass --yes")
		}
		ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), fmt.Sprintf("End %s (%s, %s in %s)? Programs in it will stop. [y/N] ", row.Name, shortID(row.ID), row.Program, row.Multiplexer))
		if err != nil || !ok {
			fmt.Fprintln(cmd.ErrOrStderr(), "Kept running.")
			return err
		}
	}
	if _, err := sessionRequest(paths.Socket, intake.Event{Type: intake.TypeSessionControl, Session: row.ID, Action: "kill", Exact: true}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Ended %s (%s)\n", row.Name, shortID(row.ID))
	return nil
}

func runSessionRename(cmd *cobra.Command, args []string) error {
	paths, db, err := pathsAndStore()
	if err != nil {
		return err
	}
	row, err := resolveSession(cmd, db, args[0])
	db.Close()
	if err != nil {
		return err
	}
	resp, err := sessionRequest(paths.Socket, intake.Event{Type: intake.TypeSessionControl, Session: row.ID, Action: "rename", Name: args[1], Exact: true})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s to %s (%s)\n", row.Name, resp.Text, shortID(row.ID))
	return nil
}

func confirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
