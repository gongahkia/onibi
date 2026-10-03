package daemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/mux"
)

var chooseMultiplexer = mux.Choose

// StartTerminalSession starts a shell, Pi, or Claude session in the configured
// terminal multiplexer. An empty requestedMux resolves from the global config
// and the optional <cwd>/.onibi.yaml project override.
func (d *Daemon) StartTerminalSession(ctx context.Context, name, agent, bin string, args []string, cwd, requestedMux string) (*Session, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s not found in PATH: %w", bin, err)
	}
	cwd, err := normalizeSessionCWD(cwd)
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	args, err = d.prepareClaudeSessionArgs(agent, args)
	if err != nil {
		return nil, err
	}
	resolved, err := config.ResolveForCWD(d.Paths, cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve multiplexer configuration: %w", err)
	}
	controller, kind, err := chooseMultiplexer(requestedMux, resolved.Config.Multiplexer, cwd)
	if err != nil {
		return nil, err
	}
	id := NewID()
	name, err = d.sessionName(name, agent)
	if err != nil {
		return nil, err
	}
	target, err := controller.Start(ctx, "onibi-"+id, mux.StartOptions{
		WindowName: name,
		CWD:        cwd,
		Env:        []string{"ONIBI_SESSION_ID=" + id, "ONIBI_SOCK=" + d.Paths.Socket},
		Command:    bin,
		Args:       args,
	})
	if err != nil {
		return nil, err
	}
	s := NewSession(id, name, agent, d.bufferSize())
	s.Transport, s.TmuxTarget, s.Cmd, s.CWD = kind, target.String(), commandLine(bin, args), cwd
	if initial, err := controller.Capture(ctx, target, 80); err == nil {
		_, _ = s.Buf.Write([]byte(initial))
	}
	if err := d.Registry.Add(s); err != nil {
		_ = controller.Kill(context.Background(), target)
		return nil, err
	}
	if d.DB != nil {
		_ = d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, s.CWD, s.Cmd, s.Transport, s.TmuxTarget, s.StartedAt())
	}
	d.audit(ctx, "session.start", s.ID, "", 0, "agent="+agent+" target="+s.TmuxTarget)
	d.Log.Info("session started", "session", s.ID, "name", s.Name, "agent", agent, "transport", kind)
	return s, nil
}

func (d *Daemon) controllerForSession(s *Session) (mux.Controller, error) {
	if s == nil {
		return nil, errors.New("session required")
	}
	if s.Transport == "codex" {
		return nil, errors.New("Codex app-server sessions have no terminal multiplexer")
	}
	resolved, err := config.ResolveForCWD(d.Paths, s.CWD)
	if err != nil {
		return nil, err
	}
	backend, err := resolved.Config.Multiplexer.Backend(s.Transport)
	if err != nil {
		return nil, err
	}
	return mux.Open(s.Transport, backend, s.CWD)
}

func (d *Daemon) muxSessionError(ctx context.Context, s *Session, err error) error {
	if err != nil && mux.IsSessionGone(err) {
		d.markSessionEndedReason(ctx, s, s.Transport+" session exited")
		return ErrSessionEnded
	}
	return err
}

func isTerminalSession(s *Session) bool { return s != nil && s.Transport != "codex" }

func muxKindLabel(kind string) string {
	switch strings.ToLower(kind) {
	case mux.Tmux:
		return "tmux"
	case mux.Zellij:
		return "Zellij"
	case mux.Screen:
		return "GNU Screen"
	default:
		return kind
	}
}
