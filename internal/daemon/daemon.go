package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/store"
)

var ErrSessionEnded = errors.New("session ended")

type Daemon struct {
	Paths            config.Paths
	DB               *store.DB
	Log              *slog.Logger
	Registry         *Registry
	Intake           *intake.Server
	LivenessInterval time.Duration
	ShellDefault     string
	ShellLogin       bool
	health           *healthTracker
	attention        *attentionTracker
	started          time.Time
	SkipRestore      bool
}

type Options struct {
	Paths            config.Paths
	DB               *store.DB
	Log              *slog.Logger
	LivenessInterval time.Duration
	ShellDefault     string
	ShellLogin       bool
	SkipRestore      bool
}

func New(opts Options) *Daemon {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.LivenessInterval <= 0 {
		opts.LivenessInterval = 5 * time.Second
	}
	d := &Daemon{Paths: opts.Paths, DB: opts.DB, Log: opts.Log, Registry: NewRegistry(), LivenessInterval: opts.LivenessInterval, ShellDefault: opts.ShellDefault, ShellLogin: opts.ShellLogin, started: time.Now(), SkipRestore: opts.SkipRestore}
	d.health = newHealthTracker(opts.DB, opts.Log)
	d.attention = newAttentionTracker()
	d.Intake = intake.New(opts.Paths.Socket, opts.Log)
	d.Intake.SetRPCHandler(d.handleRPCRequest)
	return d
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if d.DB == nil {
		return errors.New("state database required")
	}
	if d.DB != nil {
		d.health.load(ctx)
		_ = d.DB.KVPurgeExpired(ctx)
	}
	d.removeRetiredUploads()
	if !d.SkipRestore {
		d.restoreSessions(ctx)
	}
	d.Log.Info("daemon starting", "state", d.Paths.StateDir)
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.Intake.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
	}()
	wg.Add(1)
	go func() { defer wg.Done(); d.watchTmuxSessions(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); d.sweepExpiredControls(ctx) }()
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
		stop()
	}
	wg.Wait()
	if runErr != nil {
		return runErr
	}
	return ctx.Err()
}
func (d *Daemon) audit(ctx context.Context, action, sessionID, payload string, chatID int64, detail string) {
	if d.DB != nil {
		if err := d.DB.AuditAppend(ctx, action, sessionID, payload, chatID, detail); err != nil {
			d.Log.Warn("audit", "err", err)
		}
	}
	d.Log.Debug("audit", "action", action, "session", sessionID, "chat", chatID, "detail", detail)
}
func (d *Daemon) liveSessions() []*Session {
	all := d.Registry.List()
	out := make([]*Session, 0, len(all))
	for _, s := range all {
		if !s.Ended() {
			out = append(out, s)
		}
	}
	return out
}
func (d *Daemon) sessionByID(id string) (*Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrUnknownSession
	}
	var name []*Session
	var prefix []*Session
	for _, s := range d.liveSessions() {
		if s.ID == id {
			return s, nil
		}
		if strings.HasPrefix(s.ID, id) {
			prefix = append(prefix, s)
		}
		if s.Name == id {
			name = append(name, s)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return nil, errors.New("ambiguous session id")
	}
	if len(name) == 1 {
		return name[0], nil
	}
	if len(name) > 1 {
		return nil, errors.New("ambiguous session name")
	}
	return nil, ErrUnknownSession
}
func (d *Daemon) sessionForRPCTarget(id string) (*Session, error) {
	if strings.TrimSpace(id) != "" {
		if s, err := d.Registry.Get(id); err == nil && s.Ended() {
			return nil, ErrSessionEnded
		}
		return d.sessionByID(id)
	}
	list := d.liveSessions()
	if len(list) == 1 {
		return list[0], nil
	}
	if len(list) == 0 {
		return nil, ErrUnknownSession
	}
	return nil, errors.New("select a session")
}
func (d *Daemon) tmuxSessionError(ctx context.Context, s *Session, err error) error {
	if err != nil && tmuxSessionGone(err) {
		d.markSessionEndedReason(ctx, s, "tmux session exited")
		return ErrSessionEnded
	}
	return err
}
func (d *Daemon) sessionName(name, fallback string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
		for n := 2; ; n++ {
			if !d.sessionNameTaken(name) {
				return name, nil
			}
			name = fmt.Sprintf("%s-%d", fallback, n)
		}
	}
	if len(name) > 64 {
		return "", errors.New("session name exceeds 64 characters")
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return "", errors.New("session name may contain only letters, digits, dot, underscore, and hyphen")
		}
	}
	if d.sessionNameTaken(name) {
		return "", errors.New("session name already in use")
	}
	return name, nil
}
func (d *Daemon) sessionNameTaken(name string) bool {
	for _, s := range d.liveSessions() {
		if s.Name == name {
			return true
		}
	}
	return false
}
func (d *Daemon) markSessionEndedReason(ctx context.Context, s *Session, reason string) {
	if s == nil || !s.MarkEnded() {
		return
	}
	if d.DB != nil {
		_ = d.DB.SessionMarkEnded(ctx, s.ID, time.Now())
	}
	d.health.sessionEnded(ctx, s)
	d.attention.forget(s.ID)
	d.Log.Info("session ended", "session", s.ID, "name", s.Name, "agent", s.Agent, "reason", reason)
	d.audit(ctx, "session.ended", s.ID, "", 0, "")
}

func (d *Daemon) touchSession(ctx context.Context, s *Session) {
	if s == nil {
		return
	}
	s.Touch()
	if d.DB != nil {
		_ = d.DB.SessionTouch(ctx, s.ID, s.LastActivityAt())
	}
}
