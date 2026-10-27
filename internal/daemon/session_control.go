package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/gongahkia/onibi/internal/mux"
)

func (d *Daemon) SendSessionKey(ctx context.Context, id, key string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
	}
	if s.Transport != "tmux" {
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return err
		}
		if err := d.muxSessionError(ctx, s, ctrl.SendKey(ctx, mux.ParseTarget(s.TmuxTarget), key)); err != nil {
			return err
		}
		d.attention.acknowledge(s.ID)
		return nil
	}
	ctrl, err := d.tmuxControllerForCWD(s.CWD)
	if err != nil {
		return err
	}
	if err := d.tmuxSessionError(ctx, s, ctrl.SendKey(ctx, s.TmuxTarget, key)); err != nil {
		return err
	}
	d.attention.acknowledge(s.ID)
	return nil
}

// RenameSession changes the name Onibi shows and resolves. The multiplexer
// keeps its own session name, so attach commands are unaffected.
func (d *Daemon) RenameSession(ctx context.Context, s *Session, name string) (string, error) {
	d.renameMu.Lock()
	defer d.renameMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("session name is required")
	}
	if name == s.Name() {
		return name, nil
	}
	name, err := d.sessionName(name, "")
	if err != nil {
		return "", err
	}
	if d.DB != nil {
		if err := d.DB.SessionRename(ctx, s.ID, name); err != nil {
			return "", err
		}
	}
	old := s.Name()
	s.setName(name)
	d.Log.Info("session renamed", "session", s.ID, "from", old, "name", name)
	return name, nil
}

func (d *Daemon) ControlSession(ctx context.Context, id, action string) error {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return err
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
		ctrl, err := d.tmuxControllerForCWD(s.CWD)
		if err != nil {
			return err
		}
		return d.tmuxSessionError(ctx, s, ctrl.SendKey(ctx, s.TmuxTarget, "C-c"))
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
		ctrl, err := d.tmuxControllerForCWD(s.CWD)
		if err != nil {
			return err
		}
		if err := ctrl.KillSession(ctx, s.TmuxTarget); err != nil {
			return d.tmuxSessionError(ctx, s, err)
		}
		d.markSessionEndedReason(ctx, s, "ended by /kill")
		return nil
	default:
		return errors.New("unsupported action")
	}
}
