package daemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gongahkia/onibi/internal/intake"
)

func (d *Daemon) handleRPCRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	switch ev.Type {
	case intake.TypePing:
		return intake.Response{Text: d.pingText(ctx, -1)}, nil
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
		d.touchSession(ctx, s)
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
	case intake.TypeSessionNew:
		agent := strings.ToLower(strings.TrimSpace(ev.Agent))
		if agent == "" {
			return intake.Response{}, errors.New("agent required")
		}
		bin, spawnAgent, spawnArgs, ok := agentCommand(agent, ev.Args)
		if !ok {
			return intake.Response{}, errors.New("unsupported target")
		}
		path, err := exec.LookPath(bin)
		if err != nil {
			return intake.Response{}, fmt.Errorf("%s not found in PATH", bin)
		}
		s, err := d.StartTmuxSession(ctx, ev.Name, spawnAgent, path, spawnArgs, ev.CWD)
		if err != nil {
			return intake.Response{}, err
		}
		mode := strings.ToLower(strings.TrimSpace(ev.Mode))
		if mode == "visible" {
			msg, err := d.ShowSession(ctx, s.ID)
			if err != nil {
				return intake.Response{SessionID: s.ID, Mode: "headless", Text: "Started headless; show failed: " + err.Error()}, nil
			}
			return intake.Response{SessionID: s.ID, Mode: "visible", Text: "Started " + s.Name + " (" + s.ID + "). " + msg}, nil
		}
		return intake.Response{SessionID: s.ID, Mode: "headless", Text: "Started " + s.Name + " (" + s.ID + ") headless."}, nil
	case intake.TypeSessionShow:
		msg, err := d.ShowSession(ctx, ev.Session)
		if err != nil {
			return intake.Response{}, err
		}
		return intake.Response{SessionID: ev.Session, Mode: "visible", Text: msg}, nil
	case intake.TypeSessionHide:
		msg, err := d.HideSession(ctx, ev.Session, ev.Mode)
		if err != nil {
			return intake.Response{}, err
		}
		mode := "headless"
		if strings.ToLower(strings.TrimSpace(ev.Mode)) == "end" {
			mode = "ended"
		}
		return intake.Response{SessionID: ev.Session, Mode: mode, Text: msg}, nil
	case intake.TypeSessionControl:
		action := strings.ToLower(strings.TrimSpace(ev.Action))
		if action != "interrupt" && action != "kill" {
			return intake.Response{}, errors.New("session_control action must be interrupt or kill")
		}
		if err := d.ControlSession(ctx, ev.Session, action); err != nil {
			return intake.Response{}, err
		}
		return intake.Response{SessionID: ev.Session, Text: action}, nil
	case intake.TypeDemoApproval:
		return d.handleDemoApprovalRequest(ctx, ev)
	case intake.TypeSnapshot:
		return d.handleSnapshotRPC(ctx, ev)
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
