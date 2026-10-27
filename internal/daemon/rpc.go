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
	case intake.TypeSessionList:
		return intake.Response{Sessions: d.SessionInfos()}, nil
	case intake.TypeSessionCapture:
		plain, ansi, err := d.CaptureSessionHistory(ctx, ev.Session, ev.Lines)
		return intake.Response{SessionID: ev.Session, Text: plain, ANSI: ansi}, err
	case intake.TypeSessionKey:
		return intake.Response{SessionID: ev.Session}, d.sendExactSessionKey(ctx, ev.Session, ev.Key)
	case intake.TypeSessionInput:
		if ev.Exact {
			return intake.Response{SessionID: ev.Session}, d.SendSessionInput(ctx, ev.Session, ev.Text, ev.Enter)
		}
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
		s, err := d.StartTerminalSession(ctx, ev.Name, name, path, args, ev.CWD, ev.Mux)
		if err != nil {
			return intake.Response{}, err
		}
		return intake.Response{SessionID: s.ID, Text: "started " + s.Name}, nil
	case intake.TypeSessionAdopt:
		s, err := d.AdoptTerminalSession(ctx, ev.Name, ev.Mux, ev.Text, ev.CWD)
		if err != nil {
			return intake.Response{}, err
		}
		return intake.Response{SessionID: s.ID, Text: "adopted " + s.Name}, nil
	case intake.TypeSessionControl:
		if ev.Exact {
			if _, err := d.exactLiveSession(ev.Session); err != nil {
				return intake.Response{}, err
			}
			if ev.Action != "interrupt" && ev.Action != "kill" {
				return intake.Response{}, intake.Reject(errors.New("unsupported action"))
			}
		}
		return intake.Response{SessionID: ev.Session, Text: ev.Action}, d.ControlSession(ctx, ev.Session, ev.Action)
	case intake.TypeAgentLifecycle:
		return d.handleAgentLifecycle(ctx, ev)
	default:
		return intake.Response{}, errors.New("unsupported rpc")
	}
}
