package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/web"
)

func (d *Daemon) updateClaudeCost(ctx context.Context, s *Session, ev intake.Event) {
	if d == nil || d.Budget == nil || s == nil || !isClaudeAgent(ev.Agent, s.Agent) {
		return
	}
	cwd := strings.TrimSpace(ev.CWD)
	if cwd == "" {
		cwd = s.CWD
	}
	cost, ok, err := d.Budget.Update(budget.SessionRef{
		SessionID:         s.ID,
		ProviderSessionID: strings.TrimSpace(ev.ProviderSessionID),
		Agent:             "claude",
		CWD:               cwd,
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			d.Log.Debug("claude cost transcript not found", slog.String("session", s.ID), slog.String("provider_session", ev.ProviderSessionID), slog.String("cwd", cwd))
			return
		}
		d.Log.Warn("claude cost parse failed", slog.String("session", s.ID), slog.String("provider_session", ev.ProviderSessionID), slog.Any("err", err))
		return
	}
	if !ok {
		return
	}
	d.Events.Publish(web.Event{Type: "cost.updated", Payload: cost})
	d.audit(ctx, "cost.update", s.ID, "", 0, "model="+cost.Model)
}

func isClaudeAgent(eventAgent, sessionAgent string) bool {
	return strings.EqualFold(strings.TrimSpace(eventAgent), "claude") || strings.EqualFold(strings.TrimSpace(sessionAgent), "claude")
}
