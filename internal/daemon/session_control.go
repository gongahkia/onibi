package daemon

import (
	"context"
	"errors"

	"github.com/gongahkia/onibi/internal/mux"
)

func (d *Daemon) SendSessionKey(ctx context.Context, id, key string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "codex" {
		return errors.New("Codex app-server sessions do not accept terminal keys")
	}
	if s.Transport != "tmux" {
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return err
		}
		return d.muxSessionError(ctx, s, ctrl.SendKey(ctx, mux.ParseTarget(s.TmuxTarget), key))
	}
	return d.tmuxSessionError(ctx, s, newTmuxController().SendKey(ctx, s.TmuxTarget, key))
}
func (d *Daemon) ControlSession(ctx context.Context, id, action string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "codex" {
		if action == "interrupt" {
			return d.interruptCodexTurn(ctx, s.ID)
		}
		if action == "kill" {
			return d.killCodexSession(ctx, s.ID)
		}
		return errors.New("unsupported action")
	}
	switch action {
	case "interrupt":
		if s.Transport != "tmux" {
			ctrl, err := d.controllerForSession(s)
			if err != nil {
				return err
			}
			return d.muxSessionError(ctx, s, ctrl.SendKey(ctx, mux.ParseTarget(s.TmuxTarget), "ctrl-c"))
		}
		return d.tmuxSessionError(ctx, s, newTmuxController().SendKey(ctx, s.TmuxTarget, "C-c"))
	case "kill":
		if s.Transport != "tmux" {
			ctrl, err := d.controllerForSession(s)
			if err != nil {
				return err
			}
			if err := ctrl.Kill(ctx, mux.ParseTarget(s.TmuxTarget)); err != nil {
				return d.muxSessionError(ctx, s, err)
			}
			d.markSessionEndedReason(ctx, s, "ended by /kill")
			return nil
		}
		if err := newTmuxController().KillSession(ctx, s.TmuxTarget); err != nil {
			return d.tmuxSessionError(ctx, s, err)
		}
		d.markSessionEndedReason(ctx, s, "ended by /kill")
		return nil
	default:
		return errors.New("unsupported action")
	}
}
