package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/store"
)

const BufferSize = 64 * 1024

var ErrSessionEnded = errors.New("session ended")

type Daemon struct {
	Paths               config.Paths
	DB                  *store.DB
	Log                 *slog.Logger
	Registry            *Registry
	Intake              *intake.Server
	Queue               *approval.Queue
	Sweeper             *approval.Sweeper
	OutputBufferSize    int
	ShellDefault        string
	ShellLogin          bool
	screenMu            sync.RWMutex
	ScreenFont          string
	ScreenFontPath      string
	TelegramToken       string
	TelegramOwnerID     int64
	TelegramOwnerUserID int64
	TelegramPair        string
	started             time.Time
	mu                  sync.Mutex
	codexMu             sync.Mutex
	codex               map[string]*codexRuntime
	codexEvents         chan CodexEvent
	agentEvents         chan AgentEvent
	SkipRestore         bool
}

type Options struct {
	Paths                  config.Paths
	DB                     *store.DB
	Log                    *slog.Logger
	ApprovalTTL            time.Duration
	ApprovalSweepInterval  time.Duration
	ApprovalMaxSubscribers int
	OutputBufferSize       int
	ShellDefault           string
	ShellLogin             bool
	ScreenFont             string
	ScreenFontPath         string
	TelegramToken          string
	TelegramOwnerID        int64
	TelegramOwnerUserID    int64
	TelegramPair           string
	SkipRestore            bool
}

func New(opts Options) *Daemon {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	d := &Daemon{Paths: opts.Paths, DB: opts.DB, Log: opts.Log, Registry: NewRegistry(), OutputBufferSize: opts.OutputBufferSize, ShellDefault: opts.ShellDefault, ShellLogin: opts.ShellLogin, ScreenFont: opts.ScreenFont, ScreenFontPath: opts.ScreenFontPath, TelegramToken: opts.TelegramToken, TelegramOwnerID: opts.TelegramOwnerID, TelegramOwnerUserID: opts.TelegramOwnerUserID, TelegramPair: opts.TelegramPair, started: time.Now(), codex: map[string]*codexRuntime{}, codexEvents: make(chan CodexEvent, 128), agentEvents: make(chan AgentEvent, 128), SkipRestore: opts.SkipRestore}
	d.Queue = approval.New(opts.DB, opts.ApprovalTTL)
	if opts.ApprovalMaxSubscribers > 0 {
		d.Queue.MaxSubscribers = opts.ApprovalMaxSubscribers
	}
	d.Queue.Log = opts.Log
	d.Sweeper = &approval.Sweeper{Queue: d.Queue, Interval: opts.ApprovalSweepInterval, Log: opts.Log}
	d.Intake = intake.New(opts.Paths.Socket, opts.Log)
	d.Intake.SetApprovalHandler(d.handleApprovalRequest)
	d.Intake.SetRPCHandler(d.handleRPCRequest)
	return d
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if strings.TrimSpace(d.TelegramToken) == "" {
		return errors.New("Telegram bot token required; run onibi telegram setup")
	}
	if d.DB == nil {
		return errors.New("state database required")
	}
	if d.DB != nil {
		_ = d.DB.KVPurgeExpired(ctx)
		if n, err := d.Queue.CancelPending(ctx, "daemon restarted"); err != nil {
			d.Log.Warn("cancel stale Pi approvals", "err", err)
		} else if n > 0 {
			d.Log.Info("cancelled stale Pi approvals", "count", n)
		}
	}
	if !d.SkipRestore {
		d.restoreSessions(ctx)
	}
	if d.DB != nil {
		_, _ = d.Queue.ExpireOverdue(ctx)
	}
	d.Log.Info("daemon starting", "owner_paired", d.TelegramOwnerID != 0, "state", d.Paths.StateDir)
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.Intake.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
	}()
	wg.Add(1)
	go func() { defer wg.Done(); d.Sweeper.Run(ctx) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runTelegramBridge(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
	}()
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
		stop()
	}
	if n, err := d.Queue.CancelPending(context.Background(), "daemon stopped"); err != nil {
		d.Log.Warn("cancel pending Pi approvals", "err", err)
	} else if n > 0 {
		d.Log.Info("cancelled pending Pi approvals", "count", n)
	}
	wg.Wait()
	if runErr != nil {
		return runErr
	}
	return ctx.Err()
}

func (d *Daemon) bufferSize() int {
	if d.OutputBufferSize < 4096 {
		return BufferSize
	}
	return d.OutputBufferSize
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
	var name []*Session
	for _, s := range d.liveSessions() {
		if s.ID == id || strings.HasPrefix(s.ID, id) {
			return s, nil
		}
		if s.Name == id {
			name = append(name, s)
		}
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
		d.markSessionEnded(ctx, s)
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
func (d *Daemon) markSessionEnded(ctx context.Context, s *Session) {
	if s == nil || !s.MarkEnded() {
		return
	}
	if d.DB != nil {
		_ = d.DB.SessionMarkEnded(ctx, s.ID, time.Now())
	}
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
func (d *Daemon) pingText(context.Context) string {
	codexSessions := 0
	for _, s := range d.liveSessions() {
		if s.Transport == "codex" {
			codexSessions++
		}
	}
	return fmt.Sprintf("onibi\nuptime=%s\nsessions=%d\ncodex_sessions=%d", time.Since(d.started).Truncate(time.Second), len(d.liveSessions()), codexSessions)
}

func (d *Daemon) handleApprovalRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	s, err := d.sessionByID(ev.Session)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: "unknown Onibi session"}, nil
	}
	agent := strings.ToLower(strings.TrimSpace(ev.Agent))
	if agent == "" {
		agent = s.Agent
	}
	if (agent != "pi" && agent != "claude") || s.Agent != agent {
		return intake.Response{Decision: "cancelled", Reason: "unsupported approval source"}, nil
	}
	req := approval.Request{SessionID: s.ID, Agent: agent, Tool: ev.Tool, Input: json.RawMessage(ev.InputJSON)}
	if ev.Approval != nil {
		req = *ev.Approval
		req.SessionID = s.ID
		req.Agent = agent
	}
	normalized, err := approval.NormalizeRequest(req)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: "invalid approval payload"}, nil
	}
	id, waiter, err := d.Queue.RequestModel(ctx, normalized)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}
	d.audit(ctx, "approval.request", s.ID, string(normalized.Input), 0, "id="+id)
	select {
	case decision := <-waiter:
		switch decision.Verdict {
		case approval.VerdictApprove:
			return intake.Response{Decision: "approve"}, nil
		case approval.VerdictExpire:
			return intake.Response{Decision: "expired", Reason: decision.Reason}, nil
		case approval.VerdictCancel:
			return intake.Response{Decision: "cancelled", Reason: decision.Reason}, nil
		default:
			return intake.Response{Decision: "deny", Reason: decision.Reason}, nil
		}
	case <-ctx.Done():
		_ = d.Queue.Cancel(context.Background(), id, "daemon shutdown")
		return intake.Response{Decision: "cancelled", Reason: "daemon shutdown"}, nil
	}
}
