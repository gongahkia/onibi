package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/tmux"
)

var newTmuxController = tmux.New

func (d *Daemon) AttachTmux(ctx context.Context, name, target string) (*Session, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("tmux target required")
	}
	ctrl := newTmuxController()
	initial, err := ctrl.Capture(ctx, target, 50)
	if err != nil {
		return nil, err
	}
	id := NewID()
	if strings.TrimSpace(name) == "" {
		name = "tmux:" + target
	}
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		if len(p) == 1 {
			switch p[0] {
			case 3:
				return 1, ctrl.SendKey(context.Background(), target, "C-c")
			case '\n', '\r':
				return 1, ctrl.SendKey(context.Background(), target, "Enter")
			case 0x1b:
				return 1, ctrl.SendKey(context.Background(), target, "Escape")
			}
		}
		text := string(p)
		enter := strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
		text = strings.TrimRight(text, "\r\n")
		if err := ctrl.SendText(context.Background(), target, text, enter); err != nil {
			return 0, err
		}
		return len(p), nil
	}, func() error {
		return ctrl.KillPane(context.Background(), target)
	}, nil)
	s := NewSession(id, name, "tmux", host, d.bufferSize())
	s.Transport = "tmux"
	s.TmuxTarget = target
	s.Cmd = "tmux attach " + target
	if _, err := s.Buf.Write([]byte(initial)); err != nil {
		return nil, err
	}
	if err := d.Registry.Add(s); err != nil {
		return nil, err
	}
	d.persistTmuxSessionStart(ctx, s)
	go d.captureTmuxLoop(ctx, ctrl, s)
	return s, nil
}

func (d *Daemon) persistTmuxSessionStart(ctx context.Context, s *Session) {
	if d.DB == nil || s == nil {
		return
	}
	if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, "", s.Cmd, "tmux", s.TmuxTarget, s.StartedAt()); err != nil {
		d.Log.Warn("persist tmux session start", "session", s.ID, "err", err)
	}
	d.audit(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s target=%s", s.Agent, s.Name, s.TmuxTarget))
}

func (d *Daemon) captureTmuxLoop(ctx context.Context, ctrl *tmux.Controller, s *Session) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.Ended() {
				return
			}
			out, err := ctrl.Capture(ctx, s.TmuxTarget, 50)
			if err != nil {
				d.markSessionEnded(ctx, s)
				return
			}
			s.Buf.Reset()
			_, _ = s.Buf.Write([]byte(out))
			s.Touch()
		}
	}
}
