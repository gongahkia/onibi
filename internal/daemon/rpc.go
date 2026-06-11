package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gongahkia/onibi/internal/intake"
)

func (d *Daemon) handleRPCRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	switch ev.Type {
	case intake.TypeSessionInput:
		s, err := d.sessionForRPCTarget(ev.Session)
		if err != nil {
			return intake.Response{}, err
		}
		if s.Host == nil {
			return intake.Response{}, errors.New("session has no writable PTY")
		}
		payload := ev.Text
		if ev.Enter && !strings.HasSuffix(payload, "\n") {
			payload += "\n"
		}
		if payload == "" {
			return intake.Response{}, errors.New("text required")
		}
		if _, err := s.Host.Write([]byte(payload)); err != nil {
			return intake.Response{}, fmt.Errorf("write PTY: %w", err)
		}
		s.Touch()
		d.noteAnomaly(ctx, "telegram.inject")
		d.audit(ctx, "mcp.session_input", s.ID, ev.Text, 0, "")
		return intake.Response{Text: "sent to " + s.Name + " (" + s.ID + ")"}, nil
	case intake.TypeSessionPeek:
		s, err := d.sessionForRPCTarget(ev.Session)
		if err != nil {
			return intake.Response{}, err
		}
		out := s.Buf.Snapshot()
		limit := ev.Limit
		if limit <= 0 || limit > 64*1024 {
			limit = 8000
		}
		if len(out) > limit {
			out = out[len(out)-limit:]
		}
		return intake.Response{Text: string(out)}, nil
	default:
		return intake.Response{}, errors.New("unknown rpc type")
	}
}

func (d *Daemon) sessionForRPCTarget(id string) (*Session, error) {
	if strings.TrimSpace(id) != "" {
		return d.sessionByID(id)
	}
	live := d.liveSessions()
	if len(live) == 1 {
		return live[0], nil
	}
	if len(live) == 0 {
		return nil, ErrUnknownSession
	}
	return nil, errAmbiguousTarget
}
