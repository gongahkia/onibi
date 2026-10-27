package daemon

import (
	"context"
	"time"

	"github.com/gongahkia/onibi/internal/mux"
)

func (d *Daemon) watchTmuxSessions(ctx context.Context) {
	d.checkTmuxSessions(ctx)
	d.observeAttention(ctx)
	ticker := time.NewTicker(d.LivenessInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.checkTmuxSessions(ctx)
			d.observeAttention(ctx)
		}
	}
}

func (d *Daemon) checkTmuxSessions(ctx context.Context) {
	for _, s := range d.liveSessions() {
		if s.TmuxTarget == "" {
			continue
		}
		var live bool
		var err error
		if s.Transport == "tmux" {
			ctrl, controllerErr := d.tmuxControllerForCWD(s.CWD)
			if controllerErr != nil {
				err = controllerErr
			} else {
				live, err = ctrl.HasSession(ctx, s.TmuxTarget)
			}
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
			d.reportHealthEvent(d.health.tmuxResult(ctx, s, false, err))
			continue
		}
		d.reportHealthEvent(d.health.tmuxResult(ctx, s, live, nil))
		if !live {
			d.markSessionEndedReason(ctx, s, s.Transport+" session exited")
		}
	}
}
