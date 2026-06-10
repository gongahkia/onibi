package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

// BufferSize is the per-session ring-buffer capacity (bytes). 64 KiB is
// generous for text-tail rendering and small enough to keep memory bounded.
const BufferSize = 64 * 1024

// Daemon is the long-running coordinator. Phase 2 model: single-session
// per daemon process (spawned via `onibi run <agent> -- <args>`). Phase 6
// will refactor to multi-session with a long-lived background daemon.
type Daemon struct {
	Paths   config.Paths
	DB      *store.DB
	Secrets *secrets.Store
	Owner   *auth.Owner
	Bot     telegram.API
	Log     *slog.Logger

	Registry   *Registry
	Intake     *intake.Server
	Idle       *IdleDetector
	Queue      *approval.Queue
	Sweeper    *approval.Sweeper
	Router     *telegram.Router
	BufferSize int

	mu       sync.Mutex
	notified map[string]bool // session id → already-fired turn-complete once
	started  time.Time

	renderMu        sync.RWMutex
	renderOverrides map[string]render.Mode

	threadMu        sync.RWMutex
	messageSessions map[messageKey]string
	defaultTargets  map[int64]string
	pendingInjects  map[int64]string
	busySessions    map[string]bool

	// pendingEdit tracks "the user just tapped Edit" so the next text
	// reply they send is treated as the edited JSON payload.
	editMu             sync.Mutex
	pendingEdits       map[int64]string // owner chat id → approval id awaiting edit
	pendingPromptEdits map[int64]string // owner chat id → prompt id awaiting edit

	ExitWhenIdle bool // interactive agent-run mode exits after hosted sessions end
}

// Options bundles construction inputs.
type Options struct {
	Paths                 config.Paths
	DB                    *store.DB
	Secrets               *secrets.Store
	Owner                 *auth.Owner
	Bot                   telegram.API
	Router                *telegram.Router // optional; if nil, daemon creates one (untied to any bot)
	Log                   *slog.Logger
	ExitWhenIdle          bool
	ApprovalTTL           time.Duration
	ApprovalSweepInterval time.Duration
	IdleThreshold         time.Duration
	IdleInterval          time.Duration
	BufferSize            int
}

// New constructs a daemon, wiring intake + registry + idle detector +
// approval queue + telegram router.
func New(opts Options) *Daemon {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	d := &Daemon{
		Paths:              opts.Paths,
		DB:                 opts.DB,
		Secrets:            opts.Secrets,
		Owner:              opts.Owner,
		Bot:                opts.Bot,
		Log:                opts.Log,
		Registry:           NewRegistry(),
		notified:           map[string]bool{},
		started:            time.Now(),
		renderOverrides:    map[string]render.Mode{},
		messageSessions:    map[messageKey]string{},
		defaultTargets:     map[int64]string{},
		pendingInjects:     map[int64]string{},
		busySessions:       map[string]bool{},
		pendingEdits:       map[int64]string{},
		pendingPromptEdits: map[int64]string{},
		ExitWhenIdle:       opts.ExitWhenIdle,
		BufferSize:         opts.BufferSize,
	}

	// approval queue + expiry sweeper
	ttl := opts.ApprovalTTL
	if ttl <= 0 {
		ttl = approval.DefaultTTL
	}
	if opts.ApprovalTTL <= 0 && opts.DB != nil {
		v, ok, _ := opts.DB.KVGetString(context.Background(), "paranoid")
		if ok && v == "1" {
			ttl = approval.ParanoidTTL
		}
	}
	d.Queue = approval.New(opts.DB, ttl)
	d.Queue.Log = opts.Log
	d.Sweeper = &approval.Sweeper{Queue: d.Queue, Log: opts.Log, Interval: opts.ApprovalSweepInterval}

	// intake server: fire-and-forget + approval RPC
	d.Intake = intake.New(opts.Paths.Socket, d.handleEvent, opts.Log)
	d.Intake.SetApprovalHandler(d.handleApprovalRequest)

	d.Idle = &IdleDetector{
		Registry:  d.Registry,
		Threshold: opts.IdleThreshold,
		Interval:  opts.IdleInterval,
		OnIdle:    d.onIdle,
	}

	// telegram router: owner-chokepoint + callback + reply dispatch.
	// Caller may pre-build the router so they can wire router.Dispatch
	// as the bot's DefaultHandler (recommended); otherwise we build one
	// here that won't be reachable from the bot.
	if opts.Router != nil {
		d.Router = opts.Router
	} else {
		d.Router = &telegram.Router{Owner: opts.Owner, Log: opts.Log}
	}
	d.Router.OnCB = d.onCallback
	d.Router.OnReply = d.onReply
	d.Router.OnText = d.onText
	return d
}

// SpawnAgent forks an agent process under a fresh PTY, registers it with
// the registry, and starts the output-reader goroutine. envExtra is added
// to the child environment (ONIBI_SOCK + ONIBI_SESSION_ID are always added).
func (d *Daemon) SpawnAgent(ctx context.Context, name, agent, bin string, args []string, envExtra []string) (*Session, error) {
	id := NewID()
	if name == "" {
		name = agent
	}

	env := append([]string(nil), envExtra...)
	env = append(env,
		"ONIBI_SOCK="+d.Paths.Socket,
		"ONIBI_SESSION_ID="+id,
	)

	host, err := pty.Spawn(ctx, pty.SpawnOptions{
		Name: bin,
		Args: args,
		Env:  env,
	})
	if err != nil {
		return nil, err
	}
	bufSize := d.bufferSize()
	s := NewSession(id, name, agent, host, bufSize)
	s.Cmd = commandLine(bin, args)
	if err := d.Registry.Add(s); err != nil {
		_ = host.Close()
		return nil, err
	}
	d.persistSessionStart(ctx, s, "")

	go d.readLoop(s)
	go d.waitHost(s)
	return s, nil
}

func (d *Daemon) bufferSize() int {
	if d == nil || d.BufferSize <= 0 {
		return BufferSize
	}
	return d.BufferSize
}

// readLoop copies the PTY output through the ring buffer and stdout (so the
// user still sees their session live in the terminal). Touches the session
// on every read for the idle detector.
func (d *Daemon) readLoop(s *Session) {
	buf := make([]byte, 4096)
	for {
		n, err := s.Host.Master.Read(buf)
		if n > 0 {
			_, _ = s.Buf.Write(buf[:n])
			_, _ = os.Stdout.Write(buf[:n]) // mirror to user's tty
			s.Touch()
			// new activity means a future turn-complete should fire again
			d.mu.Lock()
			delete(d.notified, s.ID)
			d.mu.Unlock()
		}
		if err != nil {
			d.markSessionEnded(context.Background(), s)
			return
		}
	}
}

func (d *Daemon) waitHost(s *Session) {
	if s == nil || s.Host == nil {
		return
	}
	_ = s.Host.Wait()
	d.markSessionEnded(context.Background(), s)
}

type messageKey struct {
	chatID int64
	msgID  int
}

func (d *Daemon) persistSessionStart(ctx context.Context, s *Session, cwd string) {
	if d.DB == nil || s == nil {
		return
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, cwd, s.Cmd, "pty", "", s.StartedAt()); err != nil {
		d.Log.Warn("persist session start", slog.String("session", s.ID), slog.Any("err", err))
	}
	_ = d.DB.AuditAppend(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s", s.Agent, s.Name))
}

func (d *Daemon) markSessionEnded(ctx context.Context, s *Session) {
	if s == nil || !s.MarkEnded() {
		return
	}
	if d.DB == nil {
		return
	}
	if n, err := d.DB.PromptFailQueued(ctx, s.ID); err == nil && n > 0 {
		_ = d.DB.AuditAppend(ctx, "prompt.failed", s.ID, "", 0, fmt.Sprintf("%d queued prompt(s) failed: session ended", n))
	}
	if err := d.DB.SessionMarkEnded(ctx, s.ID, time.Now()); err != nil {
		d.Log.Warn("persist session end", slog.String("session", s.ID), slog.Any("err", err))
	}
	_ = d.DB.AuditAppend(ctx, "session.end", s.ID, "", 0, "ended")
}

// Run starts intake + idle detector + (if running interactively) waits for
// the first session's child to exit. Designed to run in `onibi run` mode.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// catch SIGINT / SIGTERM for clean shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		d.Log.Info("shutdown signal: closing socket and sessions")
		cancel()
	}()

	if err := d.RestorePendingApprovals(ctx); err != nil {
		d.Log.Warn("restore pending approvals", slog.Any("err", err))
	}
	if err := telegram.RegisterCommands(ctx, d.Bot); err != nil {
		d.Log.Warn("register telegram commands", slog.Any("err", err))
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.Intake.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("intake server", slog.Any("err", err))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.Idle.Run(ctx)
	}()

	// approval expiry sweeper — marks pending approvals as expired after
	// TTL, notifying any blocked hook so Claude unblocks promptly
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.Sweeper.Run(ctx)
	}()

	// telegram long-poll loop — Router applied to every inbound update via
	// the client's DefaultHandler (set up before bot construction below)
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.Bot.Start(ctx)
	}()

	if d.ExitWhenIdle {
		// wait for hosted session children to exit in interactive agent-run mode
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.waitForAllSessionsToExit(ctx)
			cancel()
		}()
	}

	wg.Wait()
	return nil
}

// waitForAllSessionsToExit polls registry; when all hosts have exited and
// registry is empty (or all marked ended), we return. In Phase 2 a single-
// session run; in Phase 6 this becomes a long-running idle loop.
func (d *Daemon) waitForAllSessionsToExit(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			all := d.Registry.List()
			if len(all) == 0 {
				return
			}
			anyAlive := false
			for _, s := range all {
				if !s.Ended() {
					anyAlive = true
					break
				}
			}
			if !anyAlive {
				return
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Event handlers
// ----------------------------------------------------------------------------

// handleEvent is the intake socket's dispatch entry.
func (d *Daemon) handleEvent(ctx context.Context, ev intake.Event) error {
	switch ev.Type {
	case intake.TypeAgentDone, intake.TypeAgentAwaiting:
		s := d.sessionForEvent(ev)
		d.appendEventOutput(s, ev)
		if s == nil {
			return nil
		}
		return d.notifyTurnComplete(ctx, s.ID, ev.Type, ev.Text)
	case intake.TypeAgentMessage:
		s := d.sessionForEvent(ev)
		d.appendEventOutput(s, ev)
		return nil
	case intake.TypeCmdDone:
		return d.notifyCmdDone(ctx, ev)
	case intake.TypeSessionExited:
		s := d.sessionForEvent(ev)
		if s != nil {
			d.appendEventOutput(s, ev)
			d.markSessionEnded(ctx, s)
		}
		return nil
	default:
		// unknown / future types — ignore but log so we notice
		d.Log.Info("intake: unhandled event type", slog.String("type", ev.Type))
		return nil
	}
}

func (d *Daemon) sessionForEvent(ev intake.Event) *Session {
	id := ev.Session
	if id == "" {
		id = externalSessionID(ev)
	}
	if id == "" {
		return nil
	}
	if s, err := d.Registry.Get(id); err == nil {
		return s
	}
	agent := ev.Agent
	if agent == "" {
		agent = "external"
	}
	name := agent
	if ev.ProviderSessionID != "" {
		name += "-" + shortID(ev.ProviderSessionID)
	}
	s := NewSession(id, name, agent, nil, BufferSize)
	s.Cmd = ev.Cmd
	_ = d.Registry.Add(s)
	d.persistSessionStart(context.Background(), s, ev.CWD)
	return s
}

func commandLine(bin string, args []string) string {
	parts := append([]string{bin}, args...)
	return strings.Join(parts, " ")
}

func (d *Daemon) appendEventOutput(s *Session, ev intake.Event) {
	if s == nil || s.Buf == nil {
		return
	}
	var b strings.Builder
	if ev.EventName != "" {
		fmt.Fprintf(&b, "[%s] %s\n", ev.Agent, ev.EventName)
	}
	if ev.Tool != "" {
		fmt.Fprintf(&b, "tool: %s\n", ev.Tool)
	}
	if ev.Text != "" {
		b.WriteString(ev.Text)
		if !strings.HasSuffix(ev.Text, "\n") {
			b.WriteByte('\n')
		}
	}
	if ev.Tail != "" {
		b.WriteString(ev.Tail)
		if !strings.HasSuffix(ev.Tail, "\n") {
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 && ev.RawJSON != "" {
		raw := ev.RawJSON
		if len(raw) > 2048 {
			raw = raw[:2048] + "..."
		}
		b.WriteString(raw)
		b.WriteByte('\n')
	}
	if b.Len() > 0 {
		_, _ = s.Buf.Write([]byte(b.String()))
		s.Touch()
	}
}

func (d *Daemon) notifyCmdDone(ctx context.Context, ev intake.Event) error {
	if d.Bot == nil || d.Owner == nil {
		return nil
	}
	if d.isSnoozed(ctx, "shell") {
		return nil
	}
	cmd := strings.TrimSpace(ev.Cmd)
	if cmd == "" {
		cmd = "(unknown command)"
	}
	_, err := d.sendTextOutput(ctx, d.Bot, d.Owner.ID(), fmt.Sprintf("[shell] command done rc=%d elapsed=%s", ev.Status, time.Duration(ev.Elapsed)*time.Millisecond), cmd, "onibi-shell.txt")
	return err
}

func externalSessionID(ev intake.Event) string {
	key := ev.Agent + "|" + ev.ProviderSessionID + "|" + ev.CWD + "|" + fmt.Sprint(ev.PID)
	if strings.Trim(key, "|") == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return "ext" + hex.EncodeToString(sum[:6])
}

func shortID(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[:6]
}

// onIdle is the fallback turn-complete fired by the idle detector when no
// hook event has arrived in time.
func (d *Daemon) onIdle(s *Session) {
	_ = d.notifyTurnComplete(context.Background(), s.ID, "idle_fallback", "")
}

// notifyTurnComplete formats and sends one Telegram message for the
// session's recent output, with a once-per-active-period guard so the
// hook path and the idle fallback don't both fire.
func (d *Daemon) notifyTurnComplete(ctx context.Context, sessionID, kind, hint string) error {
	if sessionID == "" {
		return nil
	}
	if d.Bot == nil || d.Owner == nil {
		return nil
	}
	d.mu.Lock()
	if d.notified[sessionID] {
		d.mu.Unlock()
		return nil
	}
	d.notified[sessionID] = true
	d.mu.Unlock()

	s, err := d.Registry.Get(sessionID)
	if err != nil {
		return nil // session not ours — likely a different daemon's hook firing
	}
	if d.isSnoozed(ctx, s.Agent) {
		d.markSessionReady(ctx, d.Bot, s)
		return nil
	}

	header := fmt.Sprintf("[%s] turn complete (%s)", s.Name, kind)
	if hint != "" {
		header += "\n" + hint
	}
	buf := s.Buf.Snapshot()
	if render.ResolveMode(buf, d.renderOverride(s.ID)) == render.ModePNG {
		img, pngErr := render.RenderPNG(buf, render.PNGOptions{})
		if pngErr == nil {
			sent, sendErr := d.Bot.SendPhoto(ctx, &tgbot.SendPhotoParams{
				ChatID:  d.Owner.ID(),
				Caption: trimCaption(header),
				Photo: &models.InputFileUpload{
					Filename: "onibi-" + s.ID + ".png",
					Data:     bytes.NewReader(img),
				},
			})
			if sendErr == nil {
				d.bindMessage(sent, s.ID)
				d.markSessionReady(ctx, d.Bot, s)
				return nil
			}
			d.Log.Warn("send turn-complete screenshot", slog.Any("err", sendErr))
		} else {
			d.Log.Warn("render screenshot", slog.Any("err", pngErr))
		}
	}
	tail := render.TextTailBody(buf, render.Options{Lang: ""})
	sent, err := d.sendTextOutput(ctx, d.Bot, d.Owner.ID(), header, tail, "onibi-"+s.ID+".txt")
	if err == nil {
		d.bindMessage(sent, s.ID)
	}
	if err != nil {
		d.Log.Warn("send turn-complete", slog.Any("err", err))
	}
	d.markSessionReady(ctx, d.Bot, s)
	return nil
}

func trimCaption(s string) string {
	r := []rune(s)
	if len(r) <= 900 {
		return s
	}
	return string(r[:900]) + "..."
}

// SendOwner is a convenience wrapper for ad-hoc messages (welcome, errors).
func (d *Daemon) SendOwner(ctx context.Context, text string) error {
	_, err := d.Bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: d.Owner.ID(),
		Text:   text,
	})
	return err
}
