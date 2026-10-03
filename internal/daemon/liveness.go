package daemon

import (
	"context"
	"time"

	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/tmux"
)

func (d *Daemon) watchTmuxSessions(ctx context.Context) {
	d.checkTmuxSessions(ctx)
	ticker := time.NewTicker(d.LivenessInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.checkTmuxSessions(ctx)
		}
	}
}

func (d *Daemon) checkTmuxSessions(ctx context.Context) {
	ctrl := newTmuxController()
	for _, s := range d.liveSessions() {
		if s.Transport == "codex" || s.TmuxTarget == "" {
			continue
		}
		var live bool
		var err error
		if s.Transport == "tmux" {
			live, err = ctrl.HasSession(ctx, s.TmuxTarget)
		} else {
			muxCtrl, controllerErr := d.controllerForSession(s)
			if controllerErr != nil {
				err = controllerErr
			} else {
				live, err = muxCtrl.Has(ctx, mux.ParseTarget(s.TmuxTarget))
			}
		}
		if err != nil {
			d.Log.Warn("multiplexer liveness", "session", s.ID, "transport", s.Transport, "err", err)
			d.queueHealthEvent(ctx, d.health.tmuxResult(ctx, s, false, err))
			continue
		}
		d.queueHealthEvent(ctx, d.health.tmuxResult(ctx, s, live, nil))
		if !live {
			d.markSessionEndedReason(ctx, s, s.Transport+" session exited")
		}
	}
}

func tmuxScreenDimensions(ctx context.Context, ctrl *tmux.Controller, target string) (int, int) {
	cols, rows, err := ctrl.PaneSize(ctx, target)
	if err != nil {
		return 100, 26
	}
	if cols < 20 {
		cols = 20
	}
	if cols > 160 {
		cols = 160
	}
	if rows < 10 {
		rows = 10
	}
	if rows > 60 {
		rows = 60
	}
	return cols, rows
}
