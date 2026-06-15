package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
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

func (d *Daemon) StartTmuxSession(ctx context.Context, name, agent, bin string, args []string, cwd string) (*Session, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, errors.New("command required")
	}
	id := NewID()
	target := "onibi-" + shortID(id)
	if strings.TrimSpace(name) == "" {
		name = agent
	}
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	ctrl := newTmuxController()
	env := []string{
		"ONIBI_SOCK=" + d.Paths.Socket,
		"ONIBI_SESSION_ID=" + id,
	}
	if err := ctrl.StartSession(ctx, target, tmux.StartOptions{
		WindowName: name,
		CWD:        cwd,
		Env:        env,
		Command:    bin,
		Args:       args,
	}); err != nil {
		return nil, err
	}
	initial, _ := ctrl.Capture(ctx, target, 50)
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
		return ctrl.KillSession(context.Background(), target)
	}, nil)
	s := NewSession(id, name, agent, host, d.bufferSize())
	s.Transport = "tmux"
	s.TmuxTarget = target
	s.Cmd = commandLine(bin, args)
	if initial != "" {
		_, _ = s.Buf.Write([]byte(initial))
	}
	if err := d.Registry.Add(s); err != nil {
		_ = host.Close()
		return nil, err
	}
	if d.DB != nil {
		if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, cwd, s.Cmd, "tmux", s.TmuxTarget, s.StartedAt()); err != nil {
			d.Log.Warn("persist tmux session start", "session", s.ID, "err", err)
		}
	}
	d.audit(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s target=%s", s.Agent, s.Name, s.TmuxTarget))
	go d.captureTmuxLoop(ctx, ctrl, s)
	return s, nil
}

func (d *Daemon) ShowSession(ctx context.Context, id string) (string, error) {
	s, err := d.sessionByID(id)
	if err != nil {
		return "", err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return "", errors.New("session is legacy pty; visible mode requires a tmux-backed session")
	}
	msg, err := d.launchVisibleTerminal(ctx, s.TmuxTarget)
	if err == nil {
		d.audit(ctx, "session.show", s.ID, "", 0, s.TmuxTarget)
	}
	return msg, err
}

func (d *Daemon) HideSession(ctx context.Context, id, mode string) (string, error) {
	s, err := d.sessionByID(id)
	if err != nil {
		return "", err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return "", errors.New("session is legacy pty; hide requires a tmux-backed session")
	}
	ctrl := newTmuxController()
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "headless":
		if err := ctrl.DetachClients(ctx, s.TmuxTarget); err != nil {
			return "", err
		}
		d.audit(ctx, "session.hide", s.ID, "", 0, "headless")
		return "Detached visible clients. Session continues headless.", nil
	case "end", "kill":
		if err := ctrl.KillSession(ctx, s.TmuxTarget); err != nil {
			return "", err
		}
		d.markSessionEnded(ctx, s)
		d.audit(ctx, "session.hide", s.ID, "", 0, "end")
		return "Ended " + s.Name + " (" + s.ID + ").", nil
	default:
		return "", errors.New("hide mode must be headless or end")
	}
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
