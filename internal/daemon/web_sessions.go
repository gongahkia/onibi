package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/workspace"
)

func (d *Daemon) WebSessions(ctx context.Context, opts web.SessionListOptions) ([]web.SessionSummary, error) {
	if d == nil {
		return nil, errors.New("daemon unavailable")
	}
	rows, err := d.webSessionRows(ctx)
	if err != nil {
		return nil, err
	}
	if opts.Workspace != "" {
		rows, err = d.filterWorkspaceSessionRows(ctx, rows, opts.Workspace)
		if err != nil {
			return nil, err
		}
	}
	pending, err := d.pendingApprovalCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]web.SessionSummary, 0, len(rows))
	for _, row := range rows {
		cost, _, err := d.SessionCost(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, web.SessionSummary{
			ID:                    row.ID,
			Agent:                 row.Agent,
			CWD:                   row.CWD,
			StartedAt:             formatWebSessionTime(row.StartedAt),
			LastActivity:          formatWebSessionTime(row.LastActivity),
			PendingApprovalsCount: pending[row.ID],
			TokensUsed:            cost.TotalTokens,
			CostUSD:               cost.TotalUSD,
			RoleRequired:          "owner",
		})
	}
	if opts.IncludeRemote && opts.Workspace == "" {
		remote, err := d.tailnetPeerSessions(ctx)
		if err != nil {
			d.Log.Debug("tailnet peer discovery failed", "err", err)
		} else {
			out = append(out, remote...)
		}
	}
	return out, nil
}

func (d *Daemon) filterWorkspaceSessionRows(ctx context.Context, rows []store.SessionEntry, name string) ([]store.SessionEntry, error) {
	if d.DB == nil {
		return rows, nil
	}
	wsStore, err := workspace.NewDBStore(d.DB)
	if err != nil {
		return nil, err
	}
	entry, ok, err := wsStore.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	out := rows[:0]
	for _, row := range rows {
		if pathInWorkspace(row.CWD, entry.Path) {
			out = append(out, row)
		}
	}
	return out, nil
}

func pathInWorkspace(path, root string) bool {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" || root == "" {
		return false
	}
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (d *Daemon) webSessionRows(ctx context.Context) ([]store.SessionEntry, error) {
	if d.DB != nil {
		return d.DB.SessionsActive(ctx)
	}
	live := d.liveSessions()
	rows := make([]store.SessionEntry, 0, len(live))
	for _, s := range live {
		rows = append(rows, store.SessionEntry{
			ID:           s.ID,
			Name:         s.Name,
			Agent:        s.Agent,
			CWD:          s.CWD,
			Command:      s.Cmd,
			Transport:    s.Transport,
			TmuxTarget:   s.TmuxTarget,
			StartedAt:    s.StartedAt(),
			LastActivity: s.LastActivityAt(),
		})
	}
	return rows, nil
}

func (d *Daemon) pendingApprovalCounts(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	if d.Queue == nil || d.DB == nil {
		return out, nil
	}
	pending, err := d.Queue.Pending(ctx)
	if err != nil {
		return nil, err
	}
	for _, approval := range pending {
		out[approval.SessionID]++
	}
	return out, nil
}

func formatWebSessionTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
