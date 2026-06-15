package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	cpty "github.com/creack/pty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/logging"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

// runRun implements `onibi run [agent [args...]]`.
// With no args: daemon-only (listens on intake socket for hook events).
// With an agent name + optional args: spawns the agent under a PTY.
func runRun(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	bufSize, _ := cmd.Flags().GetInt("buffer")
	attachTmux, _ := cmd.Flags().GetString("attach-tmux")
	debug, _ := cmd.Flags().GetBool("debug")
	logFilePath, _ := cmd.Flags().GetString("log-file")
	argv0, _ := cmd.Flags().GetString("argv0")

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	if intake.SocketActive(paths.Socket, 200*time.Millisecond) {
		return fmt.Errorf("onibi service is already running on %s; start sessions from Telegram with /new shell, /new <agent>, or /new tmux <target>; stop the service before using foreground onibi run/shell", paths.Socket)
	}
	cfg, cfgMeta, err := config.Load(paths)
	if err != nil {
		return err
	}
	if bufSize > 0 {
		cfg.Daemon.PTYBufferBytes = bufSize
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	approvalTTL := cfg.Daemon.ApprovalTimeout.Std()
	if !cfgMeta.Explicit["daemon.approval_timeout"] {
		approvalTTL = 0
	}

	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()

	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return err
	}
	token, ok, err := sec.GetWithTimeout(ctx, secrets.KeyBotToken, 30*time.Second)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no bot token stored — run `onibi setup` first")
	}
	envelopeSeed, _, err := sec.GetWithTimeout(ctx, secrets.KeyEnvelopeSeed, 30*time.Second)
	if err != nil {
		return err
	}
	logging.SetSecrets(token)

	owner, err := auth.LoadOwner(ctx, db)
	if err != nil {
		return err
	}

	if logFilePath == "" {
		logFilePath = filepath.Join(paths.LogDir, "onibi.log")
	}
	logFile, err := logging.OpenRotating(logFilePath, logging.DefaultMaxBytes, logging.DefaultBackups)
	if err != nil {
		return err
	}
	defer logFile.Close()
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	logger := logging.New(io.MultiWriter(cmd.ErrOrStderr(), logFile), level)

	router := &telegram.Router{Owner: owner, Log: logger}
	bot, err := telegram.New(ctx, telegram.Options{Token: token, APIHandler: router.Dispatch})
	if err != nil {
		return err
	}
	if want, ok, err := db.KVGetString(ctx, auth.KVKeyBotID); err != nil {
		return err
	} else if ok && want != "" && want != fmt.Sprintf("%d", bot.Self().ID) {
		return fmt.Errorf("bot identity mismatch: stored %s got %d; run `onibi rotate-token`", want, bot.Self().ID)
	}
	if bot.ClearedWebhookURL() != "" {
		logger.Warn("removed pre-existing telegram webhook")
		_, _ = bot.Send(ctx, owner.ID(), "Security notice: a pre-existing Telegram webhook was removed at startup. If this was unexpected, run `onibi rotate-token`.")
	}

	d := daemon.New(daemon.Options{
		Paths:                 paths,
		DB:                    db,
		Secrets:               sec,
		Owner:                 owner,
		Bot:                   bot,
		Router:                router,
		Log:                   logger,
		ExitWhenIdle:          len(args) > 0,
		ApprovalTTL:           approvalTTL,
		ApprovalSweepInterval: cfg.Daemon.ApprovalSweepInterval.Std(),
		IdleThreshold:         cfg.Daemon.TurnIdleThreshold.Std(),
		IdleInterval:          cfg.Daemon.TurnIdleInterval.Std(),
		BufferSize:            cfg.Daemon.PTYBufferBytes,
		EncryptedMode:         cfg.Telegram.EncryptedMode,
		MiniAppURL:            cfg.Telegram.MiniAppURL,
		EnvelopeSeed:          envelopeSeed,
		TerminalDefault:       cfg.Terminal.Default,
	})

	if attachTmux != "" {
		if len(args) > 0 {
			return errors.New("--attach-tmux cannot be combined with agent args")
		}
		s, err := d.AttachTmux(ctx, name, attachTmux)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "attached tmux target %s (session %s)\n", attachTmux, s.ID)
	} else if len(args) > 0 {
		agent := args[0]
		rest := args[1:]
		bin, err := exec.LookPath(agent)
		if err != nil {
			return fmt.Errorf("agent %q not found in PATH: %w", agent, err)
		}
		var s *daemon.Session
		if argv0 != "" {
			s, err = d.SpawnAgentWithArgv0(ctx, name, agent, bin, rest, nil, argv0)
		} else {
			s, err = d.SpawnAgent(ctx, name, agent, bin, rest, nil)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "spawned %s (session %s)\n", agent, s.ID)

		// raw mode on the user's stdin so keystrokes pass through to the
		// agent unprocessed; restore on exit. SIGWINCH → resize the child
		// PTY to match the user's terminal.
		restore, err := enterRawMode(s.Host.Master)
		if err == nil {
			defer restore()
		}
		// forward stdin → PTY (so the user can interact with the agent)
		go func() { _, _ = io.Copy(s.Host.Master, os.Stdin) }()

	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "daemon-only mode (no agent spawned); waiting for hook events on", paths.Socket)
	}

	return d.Run(ctx)
}

// enterRawMode puts the user's terminal into raw mode and starts a
// SIGWINCH handler that resizes the child PTY to match the user's window.
// Returns a restore function. No-op + nil error when stdin isn't a TTY.
func enterRawMode(master *os.File) (func(), error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	// initial resize to match
	_ = cpty.InheritSize(os.Stdin, master)

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-winch:
				_ = cpty.InheritSize(os.Stdin, master)
			case <-stop:
				return
			}
		}
	}()

	return func() {
		close(stop)
		signal.Stop(winch)
		_ = term.Restore(fd, old)
	}, nil
}
