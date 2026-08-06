package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/gongahkia/onibi/internal/intake"
)

type PiEvent struct {
	SessionID string
	Kind      string
	RunID     string
}

func (d *Daemon) PiEvents() <-chan PiEvent { return d.piEvents }

func (d *Daemon) handlePiLifecycle(ctx context.Context, ev intake.Event) (intake.Response, error) {
	if ev.Agent != "pi" {
		return intake.Response{}, errors.New("Pi lifecycle only")
	}
	kind := strings.TrimSpace(ev.Lifecycle)
	runID := strings.TrimSpace(ev.RunID)
	if (kind != "agent_start" && kind != "agent_end") || runID == "" {
		return intake.Response{}, errors.New("unsupported Pi lifecycle")
	}
	s, err := d.sessionByID(ev.Session)
	if err != nil || s.Agent != "pi" || s.Transport != "tmux" {
		return intake.Response{}, errors.New("unknown Pi session")
	}
	d.touchSession(ctx, s)
	d.audit(ctx, "pi."+kind, s.ID, "", 0, "")
	select {
	case d.piEvents <- PiEvent{SessionID: s.ID, Kind: kind, RunID: runID}:
		return intake.Response{SessionID: s.ID, Text: kind}, nil
	default:
		return intake.Response{}, errors.New("Pi lifecycle queue busy")
	}
}
