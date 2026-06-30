package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/anomaly"
	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/pushover"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/trust"
	"github.com/gongahkia/onibi/internal/web"
)

// BufferSize is the per-session ring-buffer capacity (bytes). 64 KiB is
// generous for text-tail rendering and small enough to keep memory bounded.
const BufferSize = 64 * 1024

// Daemon is the long-running coordinator. Phase 2 model: single-session
// per daemon process (spawned via `onibi run <agent> -- <args>`). Phase 6
// will refactor to multi-session with a long-lived background daemon.
type Daemon struct {
	Paths config.Paths
	DB    *store.DB
	Log   *slog.Logger

	Registry   *Registry
	Intake     *intake.Server
	Idle       *IdleDetector
	Queue      *approval.Queue
	Sweeper    *approval.Sweeper
	Events     *web.EventBus
	Trust      *trust.Watcher
	Budget     *budget.ClaudeParser
	Recorder   *web.Recorder
	BufferSize int

	TerminalDefault         string
	WebAddr                 string
	WebCertDir              string
	RelayKeys               *web.RelayKeys
	RequireWebE2E           bool
	TelegramToken           string
	TelegramOwnerID         int64
	TelegramPair            string
	Matrix                  MatrixOptions
	Slack                   SlackOptions
	Discord                 DiscordOptions
	Pushover                PushoverOptions
	Ntfy                    NtfyOptions
	Gotify                  GotifyOptions
	ProviderOutput          ProviderOutputPolicy
	ProviderOutputOverrides ProviderOutputOverrides

	mu                    sync.Mutex
	webAttachMu           sync.Mutex
	webAttachHosts        map[string]*pty.Host
	slackMu               sync.Mutex
	slackApprovals        map[string]slackApprovalRef
	notified              map[string]bool // session id → already-fired turn-complete once
	budgetDaily           map[string]int64
	budgetDailyMicroCents map[string]int64
	budgetDailyUnknown    map[string]bool
	budgetCosts           map[string]budget.CostEvent
	budgetOverruns        map[string]bool
	anomalyHistory        map[string][]anomaly.Action
	started               time.Time

	ExitWhenIdle bool // interactive agent-run mode exits after hosted sessions end
	SkipRestore  bool
}

// Options bundles construction inputs.
type Options struct {
	Paths                   config.Paths
	DB                      *store.DB
	Log                     *slog.Logger
	ExitWhenIdle            bool
	ApprovalTTL             time.Duration
	ApprovalSweepInterval   time.Duration
	IdleThreshold           time.Duration
	IdleInterval            time.Duration
	BufferSize              int
	TerminalDefault         string
	WebAddr                 string
	WebCertDir              string
	RelayKeys               *web.RelayKeys
	RequireWebE2E           bool
	TelegramToken           string
	TelegramOwnerID         int64
	TelegramPair            string
	Matrix                  MatrixOptions
	Slack                   SlackOptions
	Discord                 DiscordOptions
	Pushover                PushoverOptions
	Ntfy                    NtfyOptions
	Gotify                  GotifyOptions
	ProviderOutput          ProviderOutputPolicy
	ProviderOutputOverrides ProviderOutputOverrides
	Budget                  *budget.ClaudeParser
	Recorder                *web.Recorder
	SkipRestore             bool
}

type MatrixOptions struct {
	Homeserver     string
	AccessToken    string
	RoomID         string
	OwnerUserID    string
	AllowEncrypted bool
}

type SlackOptions struct {
	AppToken        string
	BotToken        string
	AllowedIDs      []string
	AllowedDMUsers  []string
	ApprovalChannel string
}

type DiscordOptions struct {
	Token      string
	GatewayURL string
	AllowedIDs []string
	Intents    int
}

type PushoverOptions struct {
	Token   string
	UserKey string
}

type NtfyOptions struct {
	BaseURL string
	Topic   string
	Token   string
}

type GotifyOptions struct {
	BaseURL     string
	AppToken    string
	ClientToken string
}

// New constructs a daemon, wiring intake + registry + idle detector +
// approval queue + local web cockpit.
func New(opts Options) *Daemon {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	budgetParser := opts.Budget
	if budgetParser == nil {
		budgetParser = budget.NewClaudeParser("")
	}
	recorder := opts.Recorder
	if recorder == nil && strings.TrimSpace(opts.Paths.StateDir) != "" {
		recorder = web.NewRecorder(filepath.Join(opts.Paths.StateDir, "recordings"))
	}
	d := &Daemon{
		Paths:                   opts.Paths,
		DB:                      opts.DB,
		Log:                     opts.Log,
		Registry:                NewRegistry(),
		Events:                  web.NewEventBus(),
		webAttachHosts:          map[string]*pty.Host{},
		slackApprovals:          map[string]slackApprovalRef{},
		notified:                map[string]bool{},
		budgetDaily:             map[string]int64{},
		budgetDailyMicroCents:   map[string]int64{},
		budgetDailyUnknown:      map[string]bool{},
		budgetCosts:             map[string]budget.CostEvent{},
		budgetOverruns:          map[string]bool{},
		anomalyHistory:          map[string][]anomaly.Action{},
		Budget:                  budgetParser,
		Recorder:                recorder,
		started:                 time.Now(),
		ExitWhenIdle:            opts.ExitWhenIdle,
		SkipRestore:             opts.SkipRestore,
		BufferSize:              opts.BufferSize,
		TerminalDefault:         opts.TerminalDefault,
		WebAddr:                 opts.WebAddr,
		WebCertDir:              opts.WebCertDir,
		RelayKeys:               opts.RelayKeys,
		RequireWebE2E:           opts.RequireWebE2E,
		TelegramToken:           opts.TelegramToken,
		TelegramOwnerID:         opts.TelegramOwnerID,
		TelegramPair:            opts.TelegramPair,
		Matrix:                  opts.Matrix,
		Slack:                   opts.Slack,
		Discord:                 opts.Discord,
		Pushover:                opts.Pushover,
		Ntfy:                    opts.Ntfy,
		Gotify:                  opts.Gotify,
		ProviderOutput:          opts.ProviderOutput.normalized(),
		ProviderOutputOverrides: opts.ProviderOutputOverrides,
	}

	// approval queue + expiry sweeper
	ttl := opts.ApprovalTTL
	if ttl <= 0 {
		ttl = approval.DefaultTTL
	}
	if opts.DB != nil {
		v, ok, _ := opts.DB.KVGetString(context.Background(), "paranoid")
		if ok && v == "1" && ttl > approval.ParanoidTTL {
			ttl = approval.ParanoidTTL
		}
	}
	d.Queue = approval.New(opts.DB, ttl)
	d.Queue.Log = opts.Log
	d.Sweeper = &approval.Sweeper{Queue: d.Queue, Log: opts.Log, Interval: opts.ApprovalSweepInterval}

	// intake server: fire-and-forget + approval RPC
	d.Intake = intake.New(opts.Paths.Socket, d.handleEvent, opts.Log)
	d.Intake.SetApprovalHandler(d.handleApprovalRequest)
	d.Intake.SetRPCHandler(d.handleRPCRequest)

	d.Idle = &IdleDetector{
		Registry:  d.Registry,
		Threshold: opts.IdleThreshold,
		Interval:  opts.IdleInterval,
		OnIdle:    d.onIdle,
	}

	return d
}

// SpawnAgent forks an agent process under a fresh PTY, registers it with
// the registry, and starts the output-reader goroutine. envExtra is added
// to the child environment (ONIBI_SOCK + ONIBI_SESSION_ID are always added).
func (d *Daemon) SpawnAgent(ctx context.Context, name, agent, bin string, args []string, envExtra []string) (*Session, error) {
	return d.spawnAgent(ctx, name, agent, bin, args, envExtra, "")
}

// SpawnAgentWithArgv0 is SpawnAgent with an argv[0] override. This is used
// for shells whose login mode is selected by a leading '-' in argv[0].
func (d *Daemon) SpawnAgentWithArgv0(ctx context.Context, name, agent, bin string, args []string, envExtra []string, argv0 string) (*Session, error) {
	return d.spawnAgent(ctx, name, agent, bin, args, envExtra, argv0)
}

func (d *Daemon) spawnAgent(ctx context.Context, name, agent, bin string, args []string, envExtra []string, argv0 string) (*Session, error) {
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
		Name:  bin,
		Args:  args,
		Argv0: argv0,
		Env:   env,
	})
	if err != nil {
		return nil, err
	}
	bufSize := d.bufferSize()
	s := NewSession(id, name, agent, host, bufSize)
	cwd, _ := os.Getwd()
	s.CWD = cwd
	if argv0 != "" {
		s.Cmd = commandLine(argv0, args)
	} else {
		s.Cmd = commandLine(bin, args)
	}
	if err := d.Registry.Add(s); err != nil {
		_ = host.Close()
		return nil, err
	}
	d.persistSessionStart(ctx, s, cwd)
	d.startRecording(s)

	go d.readLoop(s)
	go d.waitHost(s)
	return s, nil
}

func (d *Daemon) startRecording(s *Session) {
	if d == nil || d.Recorder == nil || s == nil || s.Host == nil {
		return
	}
	if err := d.Recorder.Record(context.Background(), s.ID, s.Host); err != nil {
		d.Log.Warn("start recording", slog.String("session", s.ID), slog.Any("err", err))
	}
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
	_, ch, unsub := s.Host.Subscribe(context.Background(), 0)
	defer unsub()
	for p := range ch {
		if len(p) > 0 {
			_, _ = s.Buf.Write(p)
			_, _ = os.Stdout.Write(p) // mirror to user's tty
			d.touchSession(context.Background(), s)
			// new activity means a future turn-complete should fire again
			d.mu.Lock()
			delete(d.notified, s.ID)
			d.mu.Unlock()
		}
	}
	d.markSessionEnded(context.Background(), s)
}

func (d *Daemon) waitHost(s *Session) {
	if s == nil || s.Host == nil {
		return
	}
	_ = s.Host.Wait()
	d.markSessionEnded(context.Background(), s)
}

func (d *Daemon) persistSessionStart(ctx context.Context, s *Session, cwd string) {
	if d.DB == nil || s == nil {
		return
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	s.CWD = cwd
	transport := s.Transport
	if transport == "" {
		transport = "pty"
	}
	if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, cwd, s.Cmd, transport, s.TmuxTarget, s.StartedAt()); err != nil {
		d.Log.Warn("persist session start", slog.String("session", s.ID), slog.Any("err", err))
	}
	d.audit(ctx, "session.start", s.ID, "", 0, fmt.Sprintf("agent=%s name=%s", s.Agent, s.Name))
}

func (d *Daemon) touchSession(ctx context.Context, s *Session) {
	if s == nil {
		return
	}
	s.Touch()
	if d.DB != nil {
		if err := d.DB.SessionTouch(ctx, s.ID, s.LastActivityAt()); err != nil {
			d.Log.Warn("persist session activity", slog.String("session", s.ID), slog.Any("err", err))
		}
	}
}

func (d *Daemon) markSessionEnded(ctx context.Context, s *Session) {
	if s == nil || !s.MarkEnded() {
		return
	}
	d.clearDefaultTargetsForSession(ctx, s.ID)
	if d.DB == nil {
		return
	}
	if n, err := d.DB.PromptFailQueued(ctx, s.ID); err == nil && n > 0 {
		d.audit(ctx, "prompt.failed", s.ID, "", 0, fmt.Sprintf("%d queued prompt(s) failed: session ended", n))
	}
	if err := d.DB.SessionMarkEnded(ctx, s.ID, time.Now()); err != nil {
		d.Log.Warn("persist session end", slog.String("session", s.ID), slog.Any("err", err))
	}
	d.audit(ctx, "session.end", s.ID, "", 0, "ended")
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

	var wg sync.WaitGroup
	if err := d.startTrustWatcher(ctx, &wg); err != nil {
		return err
	}

	if d.WebAddr != "" {
		certDir := d.WebCertDir
		if certDir == "" {
			certDir = filepath.Join(d.Paths.StateDir, "web")
		}
		cert, err := web.GenerateOrLoadCert(certDir)
		if err != nil {
			return err
		}
		webServer := web.New(web.Options{
			TLSCert:         cert,
			DB:              d.DB,
			ApprovalQueue:   d.Queue,
			EventBus:        d.Events,
			PTYHosts:        d.webPTYHosts,
			SessionIDs:      d.webSessionIDs,
			SessionList:     d.WebSessions,
			PTYHost:         d.EnsureWebPTYHost,
			Handover:        d.HandoverSession,
			Scroll:          d.ScrollSession,
			TrustRuntime:    d.AddRuntimeTrustRule,
			AnomalyAllow:    d.AddAnomalyAllowlistRule,
			SessionCost:     d.SessionCost,
			Snapshots:       d.WebSnapshots,
			SnapshotRestore: d.WebRestoreSnapshot,
			SnapshotFork:    d.WebForkSnapshot,
			RelayKeys:       d.RelayKeys,
			RequireE2E:      d.RequireWebE2E,
			Log:             d.Log,
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := webServer.StartContext(ctx, d.WebAddr); err != nil {
				d.Log.Error("web server", slog.Any("err", err))
				cancel()
			}
		}()
	}

	if strings.TrimSpace(d.TelegramToken) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.runTelegramBridge(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.Log.Error("telegram bridge", slog.Any("err", err))
				cancel()
			}
		}()
	}

	if strings.TrimSpace(d.Matrix.AccessToken) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.runMatrixBridge(ctx, matrix.New(d.Matrix.Homeserver, d.Matrix.AccessToken)); err != nil && !errors.Is(err, context.Canceled) {
				d.Log.Error("matrix bridge", slog.Any("err", err))
				cancel()
			}
		}()
	}

	if strings.TrimSpace(d.Slack.AppToken) != "" || strings.TrimSpace(d.Slack.BotToken) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.runSlackBridge(ctx, slack.New(d.Slack.AppToken, d.Slack.BotToken)); err != nil && !errors.Is(err, context.Canceled) {
				d.Log.Error("slack bridge", slog.Any("err", err))
				cancel()
			}
		}()
	}

	if strings.TrimSpace(d.Discord.Token) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.runDiscordBridge(ctx, discord.New(d.Discord.Token)); err != nil && !errors.Is(err, context.Canceled) {
				d.Log.Error("discord bridge", slog.Any("err", err))
				cancel()
			}
		}()
	}

	if strings.TrimSpace(d.Pushover.Token) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.runPushoverNotifier(ctx, pushover.New(d.Pushover.Token, d.Pushover.UserKey))
		}()
	}
	if strings.TrimSpace(d.Ntfy.Topic) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.runNtfyNotifier(ctx, ntfy.New(d.Ntfy.BaseURL, d.Ntfy.Topic, d.Ntfy.Token))
		}()
	}
	if strings.TrimSpace(d.Gotify.AppToken) != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.runGotifyNotifier(ctx, gotify.New(d.Gotify.BaseURL, d.Gotify.AppToken, d.Gotify.ClientToken))
		}()
	}

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
		d.runStartupMaintenance(ctx)
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

func (d *Daemon) webPTYHosts() map[string]*pty.Host {
	out := map[string]*pty.Host{}
	for _, s := range d.Registry.List() {
		if s.Host != nil {
			out[s.ID] = s.Host
		}
	}
	return out
}

func (d *Daemon) webSessionIDs() []string {
	live := d.liveSessions()
	out := make([]string, 0, len(live))
	for _, s := range live {
		out = append(out, s.ID)
	}
	return out
}

func (d *Daemon) runStartupMaintenance(ctx context.Context) {
	if d.DB != nil {
		if err := d.DB.KVPurgeExpired(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Warn("purge expired kv", slog.Any("err", err))
		}
	}
	if !d.SkipRestore {
		d.restoreSessions(ctx)
	}
	if err := d.RestorePendingApprovals(ctx); err != nil && !errors.Is(err, context.Canceled) {
		d.Log.Warn("restore pending approvals", slog.Any("err", err))
	}
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
		s, reason := d.sessionForEvent(ev)
		if s == nil {
			d.auditIgnoredHook(ctx, "hook.ignored", ev, reason)
			return nil
		}
		d.appendEventOutput(s, ev)
		d.updateClaudeCost(ctx, s, ev)
		return d.notifyTurnComplete(ctx, s.ID, ev.Type, ev.Text)
	case intake.TypeAgentMessage:
		s, reason := d.sessionForEvent(ev)
		if s == nil {
			d.auditIgnoredHook(ctx, "hook.ignored", ev, reason)
			return nil
		}
		d.appendEventOutput(s, ev)
		d.updateClaudeCost(ctx, s, ev)
		return nil
	case intake.TypeCmdDone:
		return d.notifyCmdDone(ctx, ev)
	case intake.TypeSessionExited:
		s, reason := d.sessionForEvent(ev)
		if s == nil {
			d.auditIgnoredHook(ctx, "hook.ignored", ev, reason)
			return nil
		}
		d.appendEventOutput(s, ev)
		d.updateClaudeCost(ctx, s, ev)
		d.markSessionEnded(ctx, s)
		return nil
	default:
		// unknown / future types — ignore but log so we notice
		d.Log.Info("intake: unhandled event type", slog.String("type", ev.Type))
		return nil
	}
}

func (d *Daemon) sessionForEvent(ev intake.Event) (*Session, string) {
	id := strings.TrimSpace(ev.Session)
	if id == "" {
		return nil, "missing Onibi session id"
	}
	if !ev.Managed {
		return nil, "unmanaged provider hook"
	}
	if s, err := d.Registry.Get(id); err == nil {
		return s, ""
	}
	return nil, "unknown Onibi session id"
}

func (d *Daemon) auditIgnoredHook(ctx context.Context, action string, ev intake.Event, reason string) {
	if reason == "" {
		reason = "unknown"
	}
	payload := ignoredHookPayload(ev)
	d.audit(ctx, action, ev.Session, payload, 0,
		fmt.Sprintf("type=%s agent=%s managed=%t provider=%s cwd=%s pid=%d reason=%s",
			ev.Type, ev.Agent, ev.Managed, ev.ProviderSessionID, ev.CWD, ev.PID, reason))
	if d.Log != nil {
		d.Log.Warn("hook ignored",
			slog.String("action", action),
			slog.String("type", ev.Type),
			slog.String("session", ev.Session),
			slog.String("agent", ev.Agent),
			slog.Bool("managed", ev.Managed),
			slog.String("provider_session", ev.ProviderSessionID),
			slog.String("cwd", ev.CWD),
			slog.Int("pid", ev.PID),
			slog.String("reason", reason))
	}
}

func ignoredHookPayload(ev intake.Event) string {
	for _, s := range []string{ev.InputJSON, ev.RawJSON, ev.Text, ev.Tail} {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
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
		d.touchSession(context.Background(), s)
	}
}

func (d *Daemon) notifyCmdDone(ctx context.Context, ev intake.Event) error {
	_ = ctx
	cmd := strings.TrimSpace(ev.Cmd)
	if cmd == "" {
		cmd = "(unknown command)"
	}
	d.Log.Info("command done", slog.Int("status", ev.Status), slog.Duration("elapsed", time.Duration(ev.Elapsed)*time.Millisecond), slog.String("cmd", cmd))
	return nil
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

// notifyTurnComplete records one turn-complete event with a
// once-per-active-period guard so the hook path and idle fallback do not
// both fire.
func (d *Daemon) notifyTurnComplete(ctx context.Context, sessionID, kind, hint string) error {
	_ = ctx
	if sessionID == "" {
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
	d.Log.Info("turn complete", slog.String("session", s.ID), slog.String("kind", kind), slog.String("hint", hint))
	return nil
}
