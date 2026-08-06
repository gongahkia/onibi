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
		return intake.Response{Text: d.pingText(ctx)}, nil
	case intake.TypeSessionInput:
		out, err := d.SendSessionTextAndCapture(ctx, ev.Session, ev.Text, ev.Enter)
		return intake.Response{SessionID: ev.Session, Text: out}, err
	case intake.TypeSessionPeek:
		out, err := d.CaptureSessionText(ctx, ev.Session)
		return intake.Response{SessionID: ev.Session, Text: out}, err
	case intake.TypeSessionNew:
		agent := strings.ToLower(strings.TrimSpace(ev.Agent))
		if agent == "codex" {
			s, err := d.StartCodexSession(ctx, ev.Name, ev.CWD)
			if err != nil {
				return intake.Response{}, err
			}
			return intake.Response{SessionID: s.ID, Text: "started Codex session"}, nil
		}
		bin, name, args, ok := d.agentCommand(agent, ev.Args)
		if !ok {
			return intake.Response{}, errors.New("supported agents: shell, codex, pi, claude")
		}
		path, err := exec.LookPath(bin)
		if err != nil {
			return intake.Response{}, fmt.Errorf("%s not found in PATH", bin)
		}
		s, err := d.StartTmuxSession(ctx, ev.Name, name, path, args, ev.CWD)
		if err != nil {
			return intake.Response{}, err
		}
		return intake.Response{SessionID: s.ID, Text: "started " + s.Name}, nil
	case intake.TypeSessionControl:
		return intake.Response{SessionID: ev.Session, Text: ev.Action}, d.ControlSession(ctx, ev.Session, ev.Action)
	case intake.TypeAgentLifecycle:
		return d.handleAgentLifecycle(ctx, ev)
	default:
		return intake.Response{}, errors.New("unsupported rpc")
	}
}
