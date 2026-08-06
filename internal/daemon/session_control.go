package daemon

import (
	"context"
	"errors"
)

func (d *Daemon) SendSessionKey(ctx context.Context, id, key string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "codex" {
		return errors.New("Codex app-server sessions do not accept terminal keys")
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
		return d.tmuxSessionError(ctx, s, newTmuxController().SendKey(ctx, s.TmuxTarget, "C-c"))
	case "kill":
		if err := newTmuxController().KillSession(ctx, s.TmuxTarget); err != nil {
			return d.tmuxSessionError(ctx, s, err)
		}
		d.markSessionEnded(ctx, s)
		return nil
	default:
		return errors.New("unsupported action")
	}
}
