package cli

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/capability"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

type firstRunPairNotice struct {
	Session store.WebSession
	Err     error
}

func runFirstRunWizard(cmd *cobra.Command, paths config.Paths, db *store.DB) error {
	cfg, meta, err := config.Load(paths)
	if err != nil {
		return err
	}
	style := styleFor(cmd)
	out := cmd.OutOrStdout()
	sc := bufio.NewScanner(cmd.InOrStdin())
	printCLIHeader(cmd, "First run")

	fmt.Fprintln(out, style.bold("Step 1/4: hooks"))
	targets, err := firstRunDetectedHookTargets(cmd, db)
	if err != nil {
		return err
	}
	selected, err := firstRunSelectHookTargets(cmd, sc, targets)
	if err != nil {
		return err
	}
	if err := firstRunInstallHookTargets(cmd, db, selected, cfg.Shell.MinDuration.Std().Milliseconds()); err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.bold("Step 2/4: transport"))
	current := cfg.Transport.Mode
	if transportFlag, _ := cmd.Flags().GetString("transport"); strings.TrimSpace(transportFlag) != "" {
		current = transportFlag
	}
	transport, err := firstRunSelectTransport(cmd, sc, current)
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, "transport.mode", transport); err != nil {
		return err
	}
	if err := config.Save(meta.Path, cfg); err != nil {
		return err
	}
	if err := cmd.Flags().Set("transport", transport); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s Transport %s\n", style.green("[OK]"), transport)

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.bold("Step 3/4: phone trust"))
	printFirstRunTrustStep(cmd, transport)

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.bold("Step 4/4: pair"))
	fmt.Fprintln(out, "The pair URL and QR print next.")
	fmt.Fprintln(out, "After pairing, use MAC and PHONE for handoff; use Esc, Tab, Ctrl, Alt, arrows, Paste, ^C, ^D, and ^Z from the soft-key bar.")
	fmt.Fprintln(out)
	return nil
}

func firstRunDetectedHookTargets(cmd *cobra.Command, db *store.DB) ([]hookTarget, error) {
	targets := make([]hookTarget, 0, len(capability.V1Agents()))
	for _, name := range capability.V1Agents() {
		agent, ok := adapters.Get(name)
		if !ok {
			return nil, adapters.Unsupported(name)
		}
		if agent.DetectPresence == nil || !agent.DetectPresence() {
			continue
		}
		target, err := agentHookTarget(cmd, db, name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func firstRunSelectHookTargets(cmd *cobra.Command, sc *bufio.Scanner, targets []hookTarget) ([]hookTarget, error) {
	out := cmd.OutOrStdout()
	if len(targets) == 0 {
		fmt.Fprintln(out, "No detected agent config dirs or shell RC files. Skipping hook install.")
		return nil, nil
	}
	style := styleFor(cmd)
	rows := [][]string{tableHeader(style, "#", "KIND", "NAME", "PATH")}
	for i, target := range targets {
		rows = append(rows, []string{strconv.Itoa(i + 1), target.Kind, target.Name, valueOrDash(target.Path)})
	}
	fmt.Fprintln(out, "Detected hooks")
	if err := renderTable(out, rows); err != nil {
		return nil, err
	}
	for {
		fmt.Fprint(out, "Install hooks [all]: ")
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return nil, err
			}
			return targets, nil
		}
		selected, err := parseFirstRunHookSelection(sc.Text(), targets)
		if err == nil {
			return selected, nil
		}
		fmt.Fprintln(out, err.Error())
	}
}

func parseFirstRunHookSelection(raw string, targets []hookTarget) ([]hookTarget, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || raw == "all" || raw == "y" || raw == "yes" {
		return targets, nil
	}
	if raw == "none" || raw == "n" || raw == "no" || raw == "skip" {
		return nil, nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) == 0 {
		return nil, nil
	}
	selected := make([]hookTarget, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		idx, ok := firstRunHookIndex(part, targets)
		if !ok {
			return nil, fmt.Errorf("choose all, none, or comma-separated numbers/names")
		}
		if !seen[idx] {
			selected = append(selected, targets[idx])
			seen[idx] = true
		}
	}
	return selected, nil
}

func firstRunHookIndex(raw string, targets []hookTarget) (int, bool) {
	if n, err := strconv.Atoi(raw); err == nil {
		idx := n - 1
		return idx, idx >= 0 && idx < len(targets)
	}
	for i, target := range targets {
		name := strings.ToLower(target.Name)
		kindName := strings.ToLower(target.Kind + ":" + target.Name)
		if raw == name || raw == kindName {
			return i, true
		}
	}
	return 0, false
}

func firstRunInstallHookTargets(cmd *cobra.Command, db *store.DB, targets []hookTarget, shellMinMS int64) error {
	if len(targets) == 0 {
		return nil
	}
	if err := adapters.LoadExternalManifests(); err != nil {
		return err
	}
	notifyBin, err := locateNotifyBinary()
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.Kind == "agent" {
			if err := installOneAgent(cmd, db, notifyBin, target.Name, false); err != nil {
				return err
			}
			continue
		}
		if err := adapters.InstallShell(cmd.Context(), db, notifyBin, target.Name, shellMinMS); err != nil {
			return err
		}
		info := adapters.ShellStatus(cmd.Context(), db, target.Name)
		fmt.Fprintf(cmd.OutOrStdout(), "%s Installed %s shell hook into %s\n", styleFor(cmd).green("[OK]"), target.Name, info.InstallPath)
	}
	return nil
}

func firstRunSelectTransport(cmd *cobra.Command, sc *bufio.Scanner, current string) (string, error) {
	return promptWebTransport(cmd, sc, current, styleFor(cmd))
}

func printFirstRunTrustStep(cmd *cobra.Command, transport string) {
	out := cmd.OutOrStdout()
	if firstRunNeedsLocalCert(transport) {
		fmt.Fprintln(out, "Install the printed onibi-local-ca.mobileconfig on iPhone if Safari warns or pairing returns Forbidden.")
		fmt.Fprintln(out, "Then enable full trust in iOS Certificate Trust Settings.")
		return
	}
	fmt.Fprintln(out, "No local CA profile step for this transport.")
}

func firstRunNeedsLocalCert(transport string) bool {
	switch normalizePairTransport(transport) {
	case "lan", "tailscale", "tailscale-private", "wireguard", "zerotier", "auto":
		return true
	default:
		return false
	}
}

func firstRunEnabled(cmd *cobra.Command) bool {
	ok, _ := cmd.Flags().GetBool("first-run")
	return ok
}

func watchFirstRunPairing(ctx context.Context, db *store.DB, baseline int) <-chan firstRunPairNotice {
	ch := make(chan firstRunPairNotice, 1)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				session, count, err := firstRunNewestOwnerSession(ctx, db)
				if err != nil {
					ch <- firstRunPairNotice{Err: err}
					return
				}
				if count > baseline {
					ch <- firstRunPairNotice{Session: session}
					return
				}
			}
		}
	}()
	return ch
}

func firstRunOwnerSessionCount(ctx context.Context, db *store.DB) (int, error) {
	_, count, err := firstRunNewestOwnerSession(ctx, db)
	return count, err
}

func firstRunNewestOwnerSession(ctx context.Context, db *store.DB) (store.WebSession, int, error) {
	sessions, err := db.ListWebSessions(ctx, false)
	if err != nil {
		return store.WebSession{}, 0, err
	}
	var newest store.WebSession
	count := 0
	for _, session := range sessions {
		if session.Role != store.PairRoleOwner {
			continue
		}
		count++
		if newest.SessionID == "" || session.CreatedAt.After(newest.CreatedAt) {
			newest = session
		}
	}
	return newest, count, nil
}

func printFirstRunPairSuccess(cmd *cobra.Command, session store.WebSession) {
	style := styleFor(cmd)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), style.green("[OK]")+" Paired "+valueOrDash(session.DeviceLabel))
	fmt.Fprintln(cmd.OutOrStdout(), "Soft keys are ready: MAC, PHONE, Esc, Tab, Ctrl, Alt, arrows, Paste, ^C, ^D, ^Z.")
}
