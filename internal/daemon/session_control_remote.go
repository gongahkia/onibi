//go:build onibi_remote

package daemon

import (
	"context"
	"errors"
	"strings"
)

func (d *Daemon) SendSessionTextAndCapture(ctx context.Context, id, text string, enter bool) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text required")
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		if err := newTmuxController().SendText(ctx, s.TmuxTarget, text, enter); err != nil {
			return "", err
		}
		d.touchSession(ctx, s)
		return "", nil
	}
	if s.Host == nil {
		return "", errors.New("session has no writable PTY")
	}
	if enter && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if _, err := s.Host.Write([]byte(text)); err != nil {
		return "", err
	}
	d.touchSession(ctx, s)
	return "", nil
}
