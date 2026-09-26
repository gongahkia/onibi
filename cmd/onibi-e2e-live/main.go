package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gotd/td/session"
	gotd "github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type liveConfig struct {
	appID       int
	appHash     string
	sessionFile string
	botUsername string
	botToken    string
}

type runningDaemon struct {
	d    *daemon.Daemon
	stop context.CancelFunc
	done <-chan error
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "live e2e:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	for _, binary := range []string{"tmux", "codex", "claude"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("%s is required: %w", binary, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	client := gotd.NewClient(cfg.appID, cfg.appHash, gotd.Options{SessionStorage: &session.FileStorage{Path: cfg.sessionFile}, NoUpdates: true})
	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("read MTProto session: %w", err)
		}
		if !status.Authorized || status.User == nil {
			return errors.New("ONIBI_E2E_SESSION_FILE must contain an authorized Telegram test-account session")
		}
		peer, err := resolveBot(ctx, client.API(), cfg.botUsername)
		if err != nil {
			return err
		}
		return runScenario(ctx, cfg, client.API(), peer, status.User.ID)
	})
}

func loadConfig() (liveConfig, error) {
	missing := make([]string, 0, 5)
	require := func(name string) string {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			missing = append(missing, name)
		}
		return value
	}
	appIDRaw := require("ONIBI_E2E_API_ID")
	cfg := liveConfig{appHash: require("ONIBI_E2E_API_HASH"), sessionFile: require("ONIBI_E2E_SESSION_FILE"), botUsername: strings.TrimPrefix(require("ONIBI_E2E_BOT_USERNAME"), "@"), botToken: require("ONIBI_E2E_BOT_TOKEN")}
	if len(missing) > 0 {
		return liveConfig{}, fmt.Errorf("missing required live-test environment: %s", strings.Join(missing, ", "))
	}
	appID, err := strconv.Atoi(appIDRaw)
	if err != nil || appID < 1 {
		return liveConfig{}, errors.New("ONIBI_E2E_API_ID must be a positive integer")
	}
	if info, err := os.Stat(cfg.sessionFile); err != nil || info.IsDir() {
		return liveConfig{}, errors.New("ONIBI_E2E_SESSION_FILE must be a readable gotd session file")
	}
	cfg.appID = appID
	return cfg, nil
}

func resolveBot(ctx context.Context, api *tg.Client, username string) (*tg.InputPeerUser, error) {
	result, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, fmt.Errorf("resolve bot @%s: %w", username, err)
	}
	peer, ok := result.Peer.(*tg.PeerUser)
	if !ok {
		return nil, fmt.Errorf("@%s is not a user bot", username)
	}
	for _, item := range result.Users {
		user, ok := item.(*tg.User)
		if !ok || user.ID != peer.UserID {
			continue
		}
		hash, ok := user.GetAccessHash()
		if !ok {
			return nil, fmt.Errorf("bot @%s has no access hash", username)
		}
		return &tg.InputPeerUser{UserID: user.ID, AccessHash: hash}, nil
	}
	return nil, fmt.Errorf("bot @%s was not present in resolve response", username)
}

func runScenario(ctx context.Context, cfg liveConfig, api *tg.Client, peer *tg.InputPeerUser, ownerID int64) error {
	root, err := os.MkdirTemp("", "onibi-e2e-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	paths := config.Paths{StateDir: root, Socket: filepath.Join(root, "onibi.sock"), DBFile: filepath.Join(root, "onibi.sqlite"), EnvFile: filepath.Join(root, ".env"), LogDir: filepath.Join(root, "logs"), Config: filepath.Join(root, "config.yaml")}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	run := startDaemon(ctx, paths, db, cfg.botToken, ownerID)
	defer stopAndCleanup(run)
	if err := exerciseTelegramShell(ctx, api, peer, run.d, root); err != nil {
		return err
	}
	if err := exerciseCodex(ctx, api, peer, run.d, root); err != nil {
		return err
	}
	if err := exerciseClaude(ctx, api, peer, run.d, root); err != nil {
		return err
	}
	run.stop()
	if err := awaitStop(ctx, run.done); err != nil {
		return err
	}
	restarted := startDaemon(ctx, paths, db, cfg.botToken, ownerID)
	defer stopAndCleanup(restarted)
	if err := exerciseRestart(ctx, api, peer, restarted.d); err != nil {
		return err
	}
	return nil
}

func startDaemon(parent context.Context, paths config.Paths, db *store.DB, token string, ownerID int64) runningDaemon {
	ctx, stop := context.WithCancel(parent)
	d := daemon.New(daemon.Options{Paths: paths, DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), TelegramToken: token, TelegramOwnerID: ownerID, TelegramOwnerUserID: ownerID, ShellDefault: "zsh", ShellLogin: false, SkipRestore: false})
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	return runningDaemon{d: d, stop: stop, done: done}
}

func cleanupSessions(d *daemon.Daemon) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, session := range d.Registry.List() {
		if !session.Ended() {
			_ = d.ControlSession(ctx, session.ID, "kill")
		}
	}
}

func stopAndCleanup(run runningDaemon) {
	cleanupSessions(run.d)
	run.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = awaitStop(ctx, run.done)
}

func awaitStop(ctx context.Context, done <-chan error) error {
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("daemon stop: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func exerciseTelegramShell(ctx context.Context, api *tg.Client, peer *tg.InputPeerUser, d *daemon.Daemon, cwd string) error {
	name := uniqueName("e2e-shell")
	if err := sendAndWait(ctx, api, peer, "/new shell --name "+name+" --cwd "+cwd, "Started "+name); err != nil {
		return fmt.Errorf("shell create: %w", err)
	}
	s, err := waitSession(ctx, d, name)
	if err != nil {
		return err
	}
	marker := uniqueName("ONIBI_E2E_SHELL_OK")
	if err := sendAndWait(ctx, api, peer, "printf '"+marker+"\\n'", "Running in "+name); err != nil {
		return fmt.Errorf("shell input: %w", err)
	}
	return waitTail(ctx, d, s.ID, marker)
}

func exerciseCodex(ctx context.Context, api *tg.Client, peer *tg.InputPeerUser, d *daemon.Daemon, cwd string) error {
	name := uniqueName("e2e-codex")
	if err := sendAndWait(ctx, api, peer, "/new codex --name "+name+" --cwd "+cwd, "Codex ready · "+name); err != nil {
		return fmt.Errorf("Codex create: %w", err)
	}
	s, err := waitSession(ctx, d, name)
	if err != nil {
		return err
	}
	marker := uniqueName("ONIBI_E2E_CODEX_OK")
	if err := sendAndWait(ctx, api, peer, "Reply with exactly "+marker+" and do not run tools.", "Codex working"); err != nil {
		return fmt.Errorf("Codex prompt: %w", err)
	}
	return waitTail(ctx, d, s.ID, marker)
}

func exerciseClaude(ctx context.Context, api *tg.Client, peer *tg.InputPeerUser, d *daemon.Daemon, cwd string) error {
	name := uniqueName("e2e-claude")
	if err := sendAndWait(ctx, api, peer, "/new claude --name "+name+" --cwd "+cwd, "Started "+name); err != nil {
		return fmt.Errorf("Claude create: %w", err)
	}
	s, err := waitSession(ctx, d, name)
	if err != nil {
		return err
	}
	marker := uniqueName("ONIBI_E2E_CLAUDE_OK")
	if err := sendAndWait(ctx, api, peer, "Reply with exactly "+marker+" and do not run tools.", "Running in "+name); err != nil {
		return fmt.Errorf("Claude prompt: %w", err)
	}
	return waitTail(ctx, d, s.ID, marker)
}

func exerciseRestart(ctx context.Context, api *tg.Client, peer *tg.InputPeerUser, d *daemon.Daemon) error {
	s, err := waitSessionPrefix(ctx, d, "e2e-shell-")
	if err != nil {
		return fmt.Errorf("restore shell: %w", err)
	}
	if err := sendAndWait(ctx, api, peer, "/target "+s.ID, "Target: "+s.Name); err != nil {
		return fmt.Errorf("restore target: %w", err)
	}
	marker := uniqueName("ONIBI_E2E_RESTART_OK")
	if err := sendAndWait(ctx, api, peer, "printf '"+marker+"\\n'", "Running in "+s.Name); err != nil {
		return fmt.Errorf("restore input: %w", err)
	}
	return waitTail(ctx, d, s.ID, marker)
}

func sendAndWait(ctx context.Context, api *tg.Client, peer *tg.InputPeerUser, input, want string) error {
	if _, err := api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{Peer: peer, Message: input, RandomID: rand.Int64()}); err != nil {
		return err
	}
	return waitMessage(ctx, api, peer, want)
}

func waitMessage(ctx context.Context, api *tg.Client, peer *tg.InputPeerUser, want string) error {
	deadline := time.NewTimer(75 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		history, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 50})
		if err != nil {
			return err
		}
		if messages, ok := history.AsModified(); ok {
			for _, raw := range messages.GetMessages() {
				message, ok := raw.(*tg.Message)
				if ok && !message.Out && strings.Contains(message.Message, want) {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for Telegram response containing %q", want)
		case <-ticker.C:
		}
	}
}

func waitSession(ctx context.Context, d *daemon.Daemon, name string) (*daemon.Session, error) {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, s := range d.Registry.List() {
			if s.Name == name && !s.Ended() {
				return s, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("timeout waiting for session %s", name)
		case <-ticker.C:
		}
	}
}

func waitSessionPrefix(ctx context.Context, d *daemon.Daemon, prefix string) (*daemon.Session, error) {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, s := range d.Registry.List() {
			if strings.HasPrefix(s.Name, prefix) && !s.Ended() {
				return s, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("timeout waiting for restored %s session", prefix)
		case <-ticker.C:
		}
	}
}

func waitTail(ctx context.Context, d *daemon.Daemon, sessionID, marker string) error {
	deadline := time.NewTimer(75 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		tail, err := d.CaptureSessionTail(ctx, sessionID, 200)
		if err == nil && strings.Contains(tail, marker) {
			return nil
		}
		if err != nil && errors.Is(err, daemon.ErrSessionEnded) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for %s output", marker)
		case <-ticker.C:
		}
	}
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%x", prefix, time.Now().UnixNano()&0xffffff)
}
