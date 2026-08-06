package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/gongahkia/onibi/internal/intake"
)

type AgentEvent struct {
	SessionID string
	Agent     string
	Kind      string
	RunID     string
}

func (d *Daemon) AgentEvents() <-chan AgentEvent { return d.agentEvents }

func (d *Daemon) handleAgentLifecycle(ctx context.Context, ev intake.Event) (intake.Response, error) {
	agent := strings.ToLower(strings.TrimSpace(ev.Agent))
	kind := strings.TrimSpace(ev.Lifecycle)
	runID := strings.TrimSpace(ev.RunID)
	switch agent {
	case "pi":
		if (kind != "agent_start" && kind != "agent_end") || runID == "" {
			return intake.Response{}, errors.New("unsupported Pi lifecycle")
		}
	case "claude":
		if kind != "agent_end" && kind != "agent_failed" {
			return intake.Response{}, errors.New("unsupported Claude lifecycle")
		}
	default:
		return intake.Response{}, errors.New("unsupported agent lifecycle")
	}
	s, err := d.sessionByID(ev.Session)
	if err != nil || s.Agent != agent || s.Transport != "tmux" {
		return intake.Response{}, errors.New("unknown agent session")
	}
	d.touchSession(ctx, s)
	d.audit(ctx, agent+"."+kind, s.ID, "", 0, "")
	select {
	case d.agentEvents <- AgentEvent{SessionID: s.ID, Agent: agent, Kind: kind, RunID: runID}:
		return intake.Response{SessionID: s.ID, Text: kind}, nil
	default:
		return intake.Response{}, errors.New("agent lifecycle queue busy")
	}
}
