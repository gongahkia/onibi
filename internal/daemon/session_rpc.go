package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/render"
)

const (
	defaultHistoryLines = 200
	maxHistoryLines     = 2000
)

// exactLiveSession resolves only a full session ID. Remote surfaces must not
// inherit the CLI's name and prefix matching, which could pick a different
// session after the intended one ends.
func (d *Daemon) exactLiveSession(id string) (*Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, intake.Reject(errors.New("an explicit session ID is required"))
	}
	s, err := d.Registry.Get(id)
	if err != nil {
		return nil, intake.Reject(ErrUnknownSession)
	}
	if s.Ended() {
		return nil, intake.Reject(ErrSessionEnded)
	}
	return s, nil
}

// SessionInfos lists live sessions in start order.
func (d *Daemon) SessionInfos() []intake.SessionInfo {
	sessions := d.liveSessions()
	out := make([]intake.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, intake.SessionInfo{
			ID: s.ID, Name: s.Name, Agent: s.Agent, Transport: s.Transport, CWD: s.CWD, Command: s.Cmd,
			StartedAt: s.StartedAt(), LastActivityAt: s.LastActivityAt(),
		})
	}
	return out
}

// CaptureSessionHistory returns up to lines of scrollback plus the visible
// screen, without focusing anything or counting the read as activity.
func (d *Daemon) CaptureSessionHistory(ctx context.Context, id string, lines int) (plain, ansi string, err error) {
	s, err := d.exactLiveSession(id)
	if err != nil {
		return "", "", err
	}
	if lines <= 0 {
		lines = defaultHistoryLines
	}
	if lines > maxHistoryLines {
		lines = maxHistoryLines
	}
	var raw string
	switch s.Transport {
	case "codex":
		raw = string(s.Buf.Snapshot())
	case "tmux":
		ctrl, err := d.tmuxControllerForCWD(s.CWD)
		if err != nil {
			return "", "", err
		}
		raw, err = ctrl.Capture(ctx, s.TmuxTarget, lines)
		if err != nil {
			return "", "", d.tmuxSessionError(ctx, s, err)
		}
	default:
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return "", "", err
		}
		raw, err = ctrl.Capture(ctx, mux.ParseTarget(s.TmuxTarget), lines)
		if err != nil {
			return "", "", d.muxSessionError(ctx, s, err)
		}
	}
	return string(render.StripANSI([]byte(raw))), raw, nil
}

// SendSessionInput sends literal text, optionally followed by Enter. Codex
// sessions accept only submitted turns.
func (d *Daemon) SendSessionInput(ctx context.Context, id, text string, enter bool) error {
	s, err := d.exactLiveSession(id)
	if err != nil {
		return err
	}
	if text == "" {
		return intake.Reject(errors.New("text required"))
	}
	if s.Transport == "codex" {
		if !enter {
			return intake.Reject(errors.New("paste mode is unavailable for Codex sessions"))
		}
		_, err := d.sendCodexTurn(ctx, s.ID, text)
		return err
	}
	return d.SendSessionText(ctx, s.ID, text, enter)
}

func (d *Daemon) sendExactSessionKey(ctx context.Context, id, key string) error {
	s, err := d.exactLiveSession(id)
	if err != nil {
		return err
	}
	if s.Transport == "codex" {
		return intake.Reject(errors.New("Codex app-server sessions do not accept terminal keys"))
	}
	normalized, err := terminalKey(key)
	if err != nil {
		return intake.Reject(err)
	}
	return d.SendSessionKey(ctx, s.ID, normalized)
}
