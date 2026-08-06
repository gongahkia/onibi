package daemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/tmux"
)

var newTmuxController = tmux.New

func (d *Daemon) StartTmuxSession(ctx context.Context, name, agent, bin string, args []string, cwd string) (*Session, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s not found in PATH: %w", bin, err)
	}
	cwd, err := normalizeSessionCWD(cwd)
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	id := NewID()
	name, err = d.sessionName(name, agent)
	if err != nil {
		return nil, err
	}
	target := "onibi-" + id
	ctrl := newTmuxController()
	if err := ctrl.StartSession(ctx, target, tmux.StartOptions{WindowName: name, CWD: cwd, Env: []string{"ONIBI_SESSION_ID=" + id, "ONIBI_SOCK=" + d.Paths.Socket}, Command: bin, Args: args}); err != nil {
		return nil, err
	}
	s := NewSession(id, name, agent, d.bufferSize())
	s.TmuxTarget, s.Cmd, s.CWD = target, commandLine(bin, args), cwd
	if initial, err := ctrl.Capture(ctx, target, 80); err == nil {
		_, _ = s.Buf.Write([]byte(initial))
	}
	if err := d.Registry.Add(s); err != nil {
		_ = ctrl.KillSession(context.Background(), target)
		return nil, err
	}
	if d.DB != nil {
		_ = d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, s.CWD, s.Cmd, "tmux", s.TmuxTarget, s.StartedAt())
	}
	d.audit(ctx, "session.start", s.ID, "", 0, "agent="+agent+" target="+target)
	d.Log.Info("session started", "session", s.ID, "name", s.Name, "agent", agent, "transport", "tmux")
	return s, nil
}

func (d *Daemon) restoreSessions(ctx context.Context) {
	if d.DB == nil {
		return
	}
	rows, err := d.DB.SessionsActive(ctx)
	if err != nil {
		d.Log.Warn("restore sessions", "err", err)
		return
	}
	ctrl := newTmuxController()
	live, err := ctrl.ListSessions(ctx)
	if err != nil {
		d.Log.Warn("tmux discovery", "err", err)
	}
	present := map[string]bool{}
	if err == nil {
		for _, s := range live {
			present[s.Name] = true
		}
	}
	for _, row := range rows {
		if row.Transport == "codex" {
			if err := d.restoreCodexSession(ctx, row); err != nil {
				d.Log.Warn("restore Codex", "session", row.ID, "err", err)
			}
			continue
		}
		if row.Transport != "tmux" {
			continue
		}
		if err != nil {
			continue
		}
		if !present[row.TmuxTarget] {
			_ = d.DB.SessionMarkEnded(ctx, row.ID, row.LastActivity)
			continue
		}
		s := newSessionAt(row.ID, row.Name, row.Agent, d.bufferSize(), row.StartedAt, row.LastActivity)
		s.TmuxTarget, s.Cmd, s.CWD = row.TmuxTarget, row.Command, row.CWD
		if out, err := ctrl.Capture(ctx, row.TmuxTarget, 80); err == nil {
			_, _ = s.Buf.Write([]byte(out))
		}
		if err := d.Registry.Add(s); err == nil {
			d.audit(ctx, "session.restore", s.ID, "", 0, "target="+s.TmuxTarget)
		}
	}
}

func (d *Daemon) CaptureSessionTail(ctx context.Context, id string, lines int) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if s.Transport != "tmux" {
		return render.TextTailBody(s.Buf.Snapshot(), render.Options{MaxLines: lines, MaxChars: 3500}), nil
	}
	if lines < 1 {
		lines = 80
	}
	if lines > 400 {
		lines = 400
	}
	out, err := newTmuxController().Capture(ctx, s.TmuxTarget, lines)
	if err != nil {
		return "", err
	}
	s.Buf.Reset()
	_, _ = s.Buf.Write([]byte(out))
	d.touchSession(ctx, s)
	return render.TextTailBody([]byte(out), render.Options{MaxLines: lines, MaxChars: 3500}), nil
}
func (d *Daemon) CaptureSessionText(ctx context.Context, id string) (string, error) {
	return d.CaptureSessionTail(ctx, id, 80)
}
func (d *Daemon) CaptureSessionScreen(ctx context.Context, id string) ([]byte, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return nil, err
	}
	if s.Transport != "tmux" {
		return render.RenderPNG(s.Buf.Snapshot(), d.screenPNGOptions(26, 100))
	}
	out, err := newTmuxController().Capture(ctx, s.TmuxTarget, 160)
	if err != nil {
		return nil, err
	}
	s.Buf.Reset()
	_, _ = s.Buf.Write([]byte(out))
	return render.RenderPNG([]byte(out), d.screenPNGOptions(26, 100))
}
func (d *Daemon) SendSessionTextAndCapture(ctx context.Context, id, text string, enter bool) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("text required")
	}
	if s.Transport == "codex" {
		if !enter {
			return "", errors.New("paste mode is unavailable for Codex sessions")
		}
		return d.sendCodexTurn(ctx, s.ID, text)
	}
	if err := newTmuxController().SendText(ctx, s.TmuxTarget, text, enter); err != nil {
		return "", err
	}
	d.touchSession(ctx, s)
	return d.CaptureSessionTail(ctx, s.ID, 80)
}
func commandLine(bin string, args []string) string {
	return strings.TrimSpace(strings.Join(append([]string{bin}, args...), " "))
}
