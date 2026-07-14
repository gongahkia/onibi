package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

var newTmuxController = tmux.New
var spawnWebPTYHost = pty.Spawn

const (
	managedSessionCaptureInterval = 2 * time.Second
	managedSessionRecoveryTimeout = 30 * time.Second
)

func (d *Daemon) AttachTmux(ctx context.Context, name, target string) (*Session, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("tmux target required")
	}
	ctrl := newTmuxController()
	initial, err := ctrl.Capture(ctx, target, 50)
	if err != nil {
		return nil, err
	}
	id := NewID()
	if strings.TrimSpace(name) == "" {
		name = "tmux:" + target
	}
	host := d.tmuxHost(ctrl, target, false)
	s := NewSession(id, name, "tmux", host, d.bufferSize())
	s.Transport = "tmux"
	s.TmuxTarget = target
	s.Cmd = "tmux attach " + target
	s.CWD = ""
	if _, err := s.Buf.Write([]byte(initial)); err != nil {
		return nil, err
	}
	if err := d.Registry.Add(s); err != nil {
		return nil, err
	}
	d.persistTmuxSessionStart(ctx, s)
	d.startRecording(s)
	go d.captureTmuxLoop(ctx, ctrl, s)
	return s, nil
}

func (d *Daemon) StartTmuxSession(ctx context.Context, name, agent, bin string, args []string, cwd string) (*Session, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, errors.New("command required")
	}
	id := NewID()
	target := "onibi-" + shortID(id)
	if strings.TrimSpace(name) == "" {
		name = agent
	}
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	ctrl := newTmuxController()
	env := []string{
		"ONIBI_SOCK=" + d.Paths.Socket,
		"ONIBI_SESSION_ID=" + id,
	}
	if err := ctrl.StartSession(ctx, target, tmux.StartOptions{
		WindowName: name,
		CWD:        cwd,
		Env:        env,
		Command:    bin,
		Args:       args,
	}); err != nil {
		return nil, err
	}
	initial, _ := ctrl.Capture(ctx, target, 50)
	host := d.tmuxHost(ctrl, target, true)
	s := NewSession(id, name, agent, host, d.bufferSize())
	s.Transport = "tmux"
	s.TmuxTarget = target
	s.Cmd = commandLine(bin, args)
	s.CWD = cwd
	if initial != "" {
		_, _ = s.Buf.Write([]byte(initial))
	}
	if err := d.Registry.Add(s); err != nil {
		_ = host.Close()
		return nil, err
	}
	if d.DB != nil {
		if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, cwd, s.Cmd, "tmux", s.TmuxTarget, s.StartedAt()); err != nil {
			d.Log.Warn("persist tmux session start", "session", s.ID, "err", err)
		}
	}
	d.audit(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s target=%s", s.Agent, s.Name, s.TmuxTarget))
	d.startRecording(s)
	go d.captureTmuxLoop(ctx, ctrl, s)
	return s, nil
}

func (d *Daemon) StartManagedTmuxSession(ctx context.Context, name, agent, bin string, args []string, cwd string) (*Session, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, errors.New("command required")
	}
	id := NewID()
	target := "onibi-" + shortID(id)
	if strings.TrimSpace(name) == "" {
		name = agent
	}
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	ctrl := newTmuxController()
	env := []string{
		"ONIBI_SOCK=" + d.Paths.Socket,
		"ONIBI_SESSION_ID=" + id,
	}
	if err := ctrl.StartSession(ctx, target, tmux.StartOptions{
		WindowName: name,
		CWD:        cwd,
		Env:        env,
		Command:    bin,
		Args:       args,
	}); err != nil {
		return nil, err
	}
	initial, _ := ctrl.Capture(ctx, target, 50)
	s := NewSession(id, name, agent, nil, d.bufferSize())
	s.Transport = "tmux"
	s.TmuxTarget = target
	s.Cmd = commandLine(bin, args)
	s.CWD = cwd
	if initial != "" {
		_, _ = s.Buf.Write([]byte(initial))
	}
	if err := d.Registry.Add(s); err != nil {
		_ = ctrl.KillSession(context.Background(), target)
		return nil, err
	}
	if d.DB != nil {
		if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, cwd, s.Cmd, "tmux", s.TmuxTarget, s.StartedAt()); err != nil {
			d.Log.Warn("persist tmux session start", "session", s.ID, "err", err)
		}
	}
	d.audit(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s target=%s", s.Agent, s.Name, s.TmuxTarget))
	go d.captureTmuxLoop(ctx, ctrl, s)
	return s, nil
}

func (d *Daemon) restoreSessions(ctx context.Context) {
	if d.DB == nil {
		return
	}
	rows, err := d.DB.SessionsActive(ctx)
	if err != nil {
		d.Log.Warn("restore sessions", slog.Any("err", err))
		return
	}
	ctrl := newTmuxController()
	discovered, discoverErr := ctrl.ListSessions(ctx)
	liveTargets := make(map[string]bool, len(discovered))
	if discoverErr == nil {
		for _, session := range discovered {
			liveTargets[session.Name] = true
		}
	} else {
		d.audit(ctx, "session.discovery.failed", "", "", 0, discoverErr.Error())
	}
	ownedTargets := make(map[string]bool, len(rows))
	for _, session := range d.Registry.List() {
		if session.Transport == "tmux" && strings.TrimSpace(session.TmuxTarget) != "" {
			ownedTargets[session.TmuxTarget] = true
		}
	}
	var restored, stale int
	for _, row := range rows {
		if _, err := d.Registry.Get(row.ID); err == nil {
			continue
		}
		if row.Transport == "tmux" && strings.TrimSpace(row.TmuxTarget) != "" {
			if ownedTargets[row.TmuxTarget] {
				d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryFailed, "duplicate tmux ownership target="+row.TmuxTarget)
				d.audit(ctx, "session.duplicate", row.ID, "", 0, "target="+row.TmuxTarget)
				stale++
				continue
			}
			d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryRecovering, "daemon restart")
			if discoverErr != nil {
				d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryReconnecting, "tmux discovery unavailable: "+discoverErr.Error())
				continue
			}
			if !liveTargets[row.TmuxTarget] {
				d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryOrphaned, "tmux target absent after discovery")
				d.audit(ctx, "session.orphaned", row.ID, "", 0, "target absent after discovery="+row.TmuxTarget)
				stale++
				continue
			}
			verified, err := managedTmuxOwnership(ctx, ctrl, row)
			if err != nil {
				d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryReconnecting, "tmux ownership lookup unavailable: "+err.Error())
				d.audit(ctx, "session.reconnecting", row.ID, "", 0, "ownership lookup target="+row.TmuxTarget+": "+err.Error())
				continue
			}
			if !verified {
				d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryOrphaned, "tmux ownership identity mismatch")
				d.audit(ctx, "session.orphaned", row.ID, "", 0, "ownership mismatch target="+row.TmuxTarget)
				stale++
				continue
			}
			if err := d.restoreTmuxSession(ctx, ctrl, row); err != nil {
				d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryReconnecting, "tmux capture unavailable: "+err.Error())
				d.audit(ctx, "session.reconnecting", row.ID, "", 0, "capture unavailable target="+row.TmuxTarget+": "+err.Error())
				stale++
				continue
			}
			d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryHealthy, "")
			ownedTargets[row.TmuxTarget] = true
			restored++
			continue
		}
		d.transitionSessionRecovery(ctx, row.ID, fleet.SessionRecoveryFailed, "unsupported restart transport="+row.Transport)
		d.audit(ctx, "session.failed", row.ID, "", 0, "unrecoverable daemon restart transport="+row.Transport)
		stale++
	}
	if discoverErr == nil {
		for target := range liveTargets {
			if strings.HasPrefix(target, "onibi-") && !ownedTargets[target] {
				d.audit(ctx, "session.discovery.unowned", "", "", 0, "target="+target)
			}
		}
	}
	if restored > 0 || stale > 0 {
		d.audit(ctx, "session.reconcile", "", "", 0, fmt.Sprintf("restored=%d stale=%d", restored, stale))
	}
}

func managedTmuxOwnership(ctx context.Context, ctrl *tmux.Controller, row store.SessionEntry) (bool, error) {
	if !strings.HasPrefix(row.TmuxTarget, "onibi-") || row.Agent == "tmux" {
		return true, nil
	}
	identity, ok, err := ctrl.SessionEnvironment(ctx, row.TmuxTarget, "ONIBI_SESSION_ID")
	if err != nil {
		return false, err
	}
	return ok && identity == row.ID, nil
}

func (d *Daemon) transitionSessionRecovery(ctx context.Context, id string, state fleet.SessionRecoveryState, reason string) {
	if d == nil || d.DB == nil {
		return
	}
	changed, err := d.DB.SessionTransitionRecovery(ctx, id, state, reason, time.Now().UTC())
	if err != nil {
		d.Log.Warn("persist session recovery", "session", id, "state", state, "err", err)
		return
	}
	if changed {
		d.audit(ctx, "session.recovery", id, "", 0, "state="+string(state)+" reason="+reason)
	}
}

func (d *Daemon) restoreTmuxSession(ctx context.Context, ctrl *tmux.Controller, row store.SessionEntry) error {
	initial, err := ctrl.Capture(ctx, row.TmuxTarget, 50)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(row.Name)
	if name == "" {
		name = "tmux:" + row.TmuxTarget
	}
	agent := strings.TrimSpace(row.Agent)
	if agent == "" {
		agent = "tmux"
	}
	s := newSessionAt(row.ID, name, agent, d.tmuxHost(ctrl, row.TmuxTarget, true), d.bufferSize(), row.StartedAt, row.LastActivity)
	s.Transport = "tmux"
	s.TmuxTarget = row.TmuxTarget
	s.Cmd = row.Command
	s.CWD = row.CWD
	if initial != "" {
		_, _ = s.Buf.Write([]byte(initial))
	}
	if err := d.Registry.Add(s); err != nil {
		return err
	}
	d.audit(ctx, "session.restore", s.ID, "", 0, "target="+s.TmuxTarget)
	d.startRecording(s)
	go d.captureTmuxLoop(ctx, ctrl, s)
	return nil
}

func (d *Daemon) tmuxHost(ctrl *tmux.Controller, target string, killSession bool) *pty.Host {
	return pty.NewVirtualHost(func(p []byte) (int, error) {
		if len(p) == 1 {
			switch p[0] {
			case 3:
				return 1, ctrl.SendKey(context.Background(), target, "C-c")
			case '\n', '\r':
				return 1, ctrl.SendKey(context.Background(), target, "Enter")
			case 0x1b:
				return 1, ctrl.SendKey(context.Background(), target, "Escape")
			}
		}
		text := string(p)
		enter := strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
		text = strings.TrimRight(text, "\r\n")
		if err := ctrl.SendText(context.Background(), target, text, enter); err != nil {
			return 0, err
		}
		return len(p), nil
	}, func() error {
		if killSession {
			return ctrl.KillSession(context.Background(), target)
		}
		return ctrl.KillPane(context.Background(), target)
	}, nil)
}

func (d *Daemon) EnsureWebPTYHost(ctx context.Context, id string) (*pty.Host, error) {
	s, err := d.sessionByID(id)
	if err != nil {
		return nil, err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		if s.Host == nil {
			return nil, errors.New("session has no writable PTY")
		}
		return s.Host, nil
	}
	for {
		d.webAttachMu.Lock()
		if d.webAttachHosts == nil {
			d.webAttachHosts = map[string]*pty.Host{}
		}
		if d.webAttachPending == nil {
			d.webAttachPending = map[string]chan struct{}{}
		}
		if h := d.webAttachHosts[s.ID]; h != nil {
			d.webAttachMu.Unlock()
			return h, nil
		}
		if pending := d.webAttachPending[s.ID]; pending != nil {
			d.webAttachMu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		pending := make(chan struct{})
		d.webAttachPending[s.ID] = pending
		d.webAttachMu.Unlock()

		ctrl := newTmuxController()
		if err := ctrl.EnablePassthrough(ctx, s.TmuxTarget); err != nil {
			d.Log.Warn("enable tmux passthrough", "session", s.ID, "target", s.TmuxTarget, "err", err)
		}
		_ = ctrl.DetachClients(ctx, s.TmuxTarget)
		args := tmuxWebAttachArgs(s.TmuxTarget)
		var host *pty.Host
		var err error
		if len(args) == 0 {
			err = errors.New("tmux attach command unavailable")
		} else {
			host, err = spawnWebPTYHost(ctx, pty.SpawnOptions{
				Name: args[0],
				Args: args[1:],
				Env:  []string{"ONIBI_SOCK=" + d.Paths.Socket, "ONIBI_SESSION_ID=" + s.ID},
				Dir:  s.CWD,
			})
		}

		d.webAttachMu.Lock()
		delete(d.webAttachPending, s.ID)
		if err == nil {
			d.webAttachHosts[s.ID] = host
		}
		close(pending)
		d.webAttachMu.Unlock()
		if err != nil {
			return nil, err
		}
		d.startRecording(&Session{ID: s.ID, Host: host})
		go func() {
			_ = host.Wait()
			d.clearWebAttachHost(s.ID, host)
		}()
		return host, nil
	}
}

func (d *Daemon) HandoverSession(ctx context.Context, id, target string) (string, error) {
	s, err := d.sessionByID(id)
	if err != nil {
		return "", err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return "", errors.New("handover requires a tmux-backed session")
	}
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "mac":
		d.closeWebAttachHost(s.ID)
		msg, err := d.ShowSession(ctx, s.ID)
		if err != nil {
			return "", err
		}
		return msg, nil
	case "phone":
		ctrl := newTmuxController()
		_ = ctrl.DetachClients(ctx, s.TmuxTarget)
		d.closeWebAttachHost(s.ID)
		if _, err := d.EnsureWebPTYHost(ctx, s.ID); err != nil {
			return "", err
		}
		d.audit(ctx, "session.handover", s.ID, "", 0, "phone")
		return "Phone handover ready.", nil
	default:
		return "", errors.New("handover target must be mac or phone")
	}
}

func (d *Daemon) KillTmuxTarget(ctx context.Context, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("tmux target required")
	}
	return newTmuxController().KillSession(ctx, target)
}

func (d *Daemon) ScrollSession(ctx context.Context, id, direction string) error {
	s, err := d.sessionByID(id)
	if err != nil {
		return err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return errors.New("scrollback requires a tmux-backed session")
	}
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "page_up":
		return newTmuxController().CopyModePageUp(ctx, s.TmuxTarget)
	case "page_down":
		return newTmuxController().CopyModePageDown(ctx, s.TmuxTarget)
	default:
		return errors.New("scroll direction must be page_up or page_down")
	}
}

func (d *Daemon) clearWebAttachHost(id string, host *pty.Host) {
	d.webAttachMu.Lock()
	defer d.webAttachMu.Unlock()
	if d.webAttachHosts[id] != host {
		return
	}
	delete(d.webAttachHosts, id)
}

func (d *Daemon) closeWebAttachHost(id string) {
	d.webAttachMu.Lock()
	host := d.webAttachHosts[id]
	delete(d.webAttachHosts, id)
	d.webAttachMu.Unlock()
	if host != nil {
		_ = host.Close()
	}
}

func (d *Daemon) ShowSession(ctx context.Context, id string) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return "", errors.New("session is legacy pty; visible mode requires a tmux-backed session")
	}
	msg, err := d.launchVisibleTerminal(ctx, s.TmuxTarget)
	if err == nil {
		d.audit(ctx, "session.show", s.ID, "", 0, s.TmuxTarget)
	}
	return msg, err
}

func (d *Daemon) HideSession(ctx context.Context, id, mode string) (string, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return "", err
	}
	if s.Transport != "tmux" || s.TmuxTarget == "" {
		return "", errors.New("session is legacy pty; hide requires a tmux-backed session")
	}
	ctrl := newTmuxController()
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "headless":
		if err := ctrl.DetachClients(ctx, s.TmuxTarget); err != nil {
			return "", err
		}
		d.closeWebAttachHost(s.ID)
		d.audit(ctx, "session.hide", s.ID, "", 0, "headless")
		return "Detached visible clients. Session continues headless.", nil
	case "end", "kill":
		d.closeWebAttachHost(s.ID)
		if err := ctrl.KillSession(ctx, s.TmuxTarget); err != nil {
			return "", err
		}
		d.markSessionEnded(ctx, s)
		d.audit(ctx, "session.hide", s.ID, "", 0, "end")
		return "Ended " + s.Name + " (" + s.ID + ").", nil
	default:
		return "", errors.New("hide mode must be headless or end")
	}
}

func (d *Daemon) persistTmuxSessionStart(ctx context.Context, s *Session) {
	if d.DB == nil || s == nil {
		return
	}
	if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, "", s.Cmd, "tmux", s.TmuxTarget, s.StartedAt()); err != nil {
		d.Log.Warn("persist tmux session start", "session", s.ID, "err", err)
	}
	d.audit(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s target=%s", s.Agent, s.Name, s.TmuxTarget))
}

func (d *Daemon) captureTmuxLoop(ctx context.Context, ctrl *tmux.Controller, s *Session) {
	interval := d.tmuxCaptureInterval
	if interval <= 0 {
		interval = managedSessionCaptureInterval
	}
	timeout := d.tmuxRecoveryTimeout
	if timeout <= 0 {
		timeout = managedSessionRecoveryTimeout
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	var recoveryDeadline time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.Ended() {
				return
			}
			out, err := ctrl.Capture(ctx, s.TmuxTarget, 50)
			if err != nil {
				now := time.Now().UTC()
				if recoveryDeadline.IsZero() {
					recoveryDeadline = now.Add(timeout)
					d.transitionSessionRecovery(ctx, s.ID, fleet.SessionRecoveryReconnecting, "tmux capture disconnected: "+err.Error())
				}
				if !recoveryDeadline.After(now) {
					d.transitionSessionRecovery(ctx, s.ID, fleet.SessionRecoveryOrphaned, "tmux reconnect timed out: "+err.Error())
					d.audit(ctx, "session.orphaned", s.ID, "", 0, "tmux reconnect timeout")
					return
				}
				continue
			}
			if !recoveryDeadline.IsZero() {
				d.transitionSessionRecovery(ctx, s.ID, fleet.SessionRecoveryHealthy, "")
				recoveryDeadline = time.Time{}
			}
			s.Buf.Reset()
			_, _ = s.Buf.Write([]byte(out))
			d.touchSession(ctx, s)
		}
	}
}
