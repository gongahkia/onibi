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
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		return newTmuxController().SendKey(ctx, s.TmuxTarget, key)
	}
	if s.Host == nil {
		return errors.New("session has no writable PTY")
	}
	switch key {
	case "Enter":
		_, err = s.Host.Write([]byte{'\n'})
	case "Escape":
		_, err = s.Host.Write([]byte{0x1b})
	default:
		err = errors.New("unsupported key")
	}
	return err
}

func (d *Daemon) ControlSession(ctx context.Context, id, action string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		ctrl := newTmuxController()
		switch action {
		case "interrupt":
			return ctrl.SendKey(ctx, s.TmuxTarget, "C-c")
		case "kill":
			if err := ctrl.KillSession(ctx, s.TmuxTarget); err != nil {
				return err
			}
			d.markSessionEnded(ctx, s)
			return nil
		default:
			return errors.New("unsupported action")
		}
	}
	if s.Host == nil {
		return errors.New("session has no writable PTY")
	}
	switch action {
	case "interrupt":
		_, err = s.Host.Write([]byte{3})
		return err
	case "kill":
		if err := s.Host.Close(); err != nil {
			return err
		}
		d.markSessionEnded(ctx, s)
		return nil
	default:
		return errors.New("unsupported action")
	}
}
