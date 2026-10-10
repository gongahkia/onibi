package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/mux"
)

var chooseMultiplexer = mux.Choose
var openMultiplexer = mux.Open

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
	if d.DB != nil {
		if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, s.CWD, s.Cmd, s.Transport, s.TmuxTarget, s.StartedAt()); err != nil {
			_ = controller.Kill(context.Background(), target)
			return nil, fmt.Errorf("persist session: %w", err)
		}
	}
	if err := d.Registry.Add(s); err != nil {
		_ = controller.Kill(context.Background(), target)
		if d.DB != nil {
			_ = d.DB.SessionMarkEnded(context.Background(), s.ID, s.StartedAt())
		}
		return nil, err
	}
	d.audit(ctx, "session.start", s.ID, "", 0, "agent="+agent+" target="+s.TmuxTarget)
	d.Log.Info("session started", "session", s.ID, "name", s.Name, "agent", agent, "transport", kind)
	return s, nil
}

// AdoptTerminalSession explicitly hands an existing mux target to Onibi.
// Unlike StartTerminalSession, errors must never kill the caller's target.
func (d *Daemon) AdoptTerminalSession(ctx context.Context, name, kind, rawTarget, cwd string) (*Session, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != mux.Tmux && kind != mux.Zellij && kind != mux.Screen {
		return nil, errors.New("supported multiplexers: tmux, zellij, screen")
	}
	rawTarget = strings.TrimSpace(rawTarget)
	if rawTarget == "" || strings.HasPrefix(rawTarget, "-") || len(rawTarget) > 128 {
		return nil, errors.New("valid multiplexer target required")
	}
	target := mux.ParseTarget(rawTarget)
	if target.Session == "" || (kind == mux.Zellij && target.Pane == "") || (kind != mux.Zellij && target.Pane != "") {
		return nil, errors.New("target must be a session name (Zellij: session|pane-id)")
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	var err error
	cwd, err = normalizeSessionCWD(cwd)
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	for _, existing := range d.liveSessions() {
		if existing.Transport == kind && existing.TmuxTarget == target.String() {
			return nil, fmt.Errorf("target already managed as %s", existing.Name)
		}
	}
	resolved, err := config.ResolveForCWD(d.Paths, cwd)
	if err != nil {
		return nil, err
	}
	backend, err := resolved.Config.Multiplexer.Backend(kind)
	if err != nil {
		return nil, err
	}
	controller, err := openMultiplexer(kind, backend, cwd)
	if err != nil {
		return nil, err
	}
	live, err := controller.Has(ctx, target)
	if err != nil {
		return nil, err
	}
	if !live {
		return nil, errors.New("multiplexer target not found")
	}
	name, err = d.sessionName(name, "attached")
	if err != nil {
		return nil, err
	}
	s := NewSession(NewID(), name, "shell", d.bufferSize())
	s.Transport, s.TmuxTarget, s.Cmd, s.CWD = kind, target.String(), "[adopted]", cwd
	if output, err := controller.Capture(ctx, target, 80); err == nil {
		_, _ = s.Buf.Write([]byte(output))
	}
	if d.DB != nil {
		if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, s.CWD, s.Cmd, s.Transport, s.TmuxTarget, s.StartedAt()); err != nil {
			return nil, fmt.Errorf("persist adopted session: %w", err)
		}
	}
	if err := d.Registry.Add(s); err != nil {
		if d.DB != nil {
			_ = d.DB.SessionMarkEnded(context.Background(), s.ID, s.StartedAt())
		}
		return nil, err
	}
	d.audit(ctx, "session.adopt", s.ID, "", 0, "target="+s.TmuxTarget)
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
