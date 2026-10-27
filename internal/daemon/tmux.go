package daemon

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

var newTmuxController = tmux.New

const legacyCodexTransport = "codex"

func (d *Daemon) restoreSessions(ctx context.Context) {
	if d.DB == nil {
		return
	}
	rows, err := d.DB.SessionsActive(ctx)
	if err != nil {
		d.Log.Warn("restore sessions", "err", err)
		return
	}
	for _, row := range rows {
		if row.Transport == legacyCodexTransport {
			// Earlier releases ran Codex App Server sessions, which cannot be
			// resumed as terminal programs.
			_ = d.DB.SessionMarkEnded(ctx, row.ID, time.Now())
			d.Log.Info("Codex App Server session not restored", "session", row.ID, "name", row.Name)
			continue
		}
		if row.Transport != "tmux" {
			continue
		}
		ctrl, err := d.tmuxControllerForCWD(row.CWD)
		if err != nil {
			d.Log.Warn("restore tmux configuration", "session", row.ID, "err", err)
			continue
		}
		live, err := ctrl.HasSession(ctx, row.TmuxTarget)
		if err != nil {
			d.Log.Warn("tmux discovery", "session", row.ID, "err", err)
			continue
		}
		if !live {
			_ = d.DB.SessionMarkEnded(ctx, row.ID, row.LastActivity)
			d.Log.Info("restored session already ended", "session", row.ID, "name", row.Name)
			continue
		}
		s := newSessionAt(row.ID, row.Name, row.Agent, row.StartedAt, row.LastActivity)
		s.TmuxTarget, s.Cmd, s.CWD = row.TmuxTarget, row.Command, row.CWD
		if err := d.Registry.Add(s); err == nil {
			d.audit(ctx, "session.restore", s.ID, "", 0, "target="+s.TmuxTarget)
		}
	}
	d.restoreOtherMuxSessions(ctx, rows)
}

func (d *Daemon) restoreOtherMuxSessions(ctx context.Context, rows []store.SessionEntry) {
	for _, row := range rows {
		if row.Transport == "tmux" || row.Transport == legacyCodexTransport || row.TmuxTarget == "" {
			continue
		}
		s := newSessionAt(row.ID, row.Name, row.Agent, row.StartedAt, row.LastActivity)
		s.Transport, s.TmuxTarget, s.Cmd, s.CWD = row.Transport, row.TmuxTarget, row.Command, row.CWD
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			d.Log.Warn("restore multiplexer", "session", row.ID, "transport", row.Transport, "err", err)
			continue
		}
		live, err := ctrl.Has(ctx, mux.ParseTarget(s.TmuxTarget))
		if err != nil {
			d.Log.Warn("multiplexer discovery", "session", row.ID, "transport", row.Transport, "err", err)
			continue
		}
		if !live {
			_ = d.DB.SessionMarkEnded(ctx, row.ID, row.LastActivity)
			d.Log.Info("restored session already ended", "session", row.ID, "name", row.Name)
			continue
		}
		if err := d.Registry.Add(s); err == nil {
			d.audit(ctx, "session.restore", s.ID, "", 0, "target="+s.TmuxTarget)
		}
	}
}

func (d *Daemon) CaptureSessionTail(ctx context.Context, id string, lines int) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if lines < 1 {
		lines = 80
	}
	if lines > 400 {
		lines = 400
	}
	if s.Transport != "tmux" {
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return "", err
		}
		out, err := ctrl.Capture(ctx, mux.ParseTarget(s.TmuxTarget), lines)
		if err != nil {
			return "", d.muxSessionError(ctx, s, err)
		}
		d.touchSession(ctx, s)
		return render.TextTailBody([]byte(out), render.Options{MaxLines: lines, MaxChars: 3500}), nil
	}
	ctrl, err := d.tmuxControllerForCWD(s.CWD)
	if err != nil {
		return "", err
	}
	out, err := ctrl.Capture(ctx, s.TmuxTarget, lines)
	if err != nil {
		return "", d.tmuxSessionError(ctx, s, err)
	}
	d.touchSession(ctx, s)
	return render.TextTailBody([]byte(out), render.Options{MaxLines: lines, MaxChars: 3500}), nil
}
func (d *Daemon) CaptureSessionText(ctx context.Context, id string) (string, error) {
	return d.CaptureSessionTail(ctx, id, 80)
}
func (d *Daemon) SendSessionText(ctx context.Context, id, text string, enter bool) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if text == "" {
		return errors.New("text required")
	}
	if s.Transport != "tmux" {
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return err
		}
		if err := ctrl.SendText(ctx, mux.ParseTarget(s.TmuxTarget), text, enter); err != nil {
			return d.muxSessionError(ctx, s, err)
		}
		d.touchSession(ctx, s)
		d.attention.acknowledge(s.ID)
		return nil
	}
	ctrl, err := d.tmuxControllerForCWD(s.CWD)
	if err != nil {
		return err
	}
	if err := ctrl.SendText(ctx, s.TmuxTarget, text, enter); err != nil {
		return d.tmuxSessionError(ctx, s, err)
	}
	d.touchSession(ctx, s)
	d.attention.acknowledge(s.ID)
	return nil
}
func (d *Daemon) SendSessionTextAndCapture(ctx context.Context, id, text string, enter bool) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", errors.New("text required")
	}
	if err := d.SendSessionText(ctx, s.ID, text, enter); err != nil {
		return "", err
	}
	return d.CaptureSessionTail(ctx, s.ID, 80)
}

func tmuxSessionGone(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no server running") || strings.Contains(text, "can't find session") || strings.Contains(text, "no such session")
}
func commandLine(bin string, args []string) string {
	return strings.TrimSpace(strings.Join(append([]string{bin}, args...), " "))
}
