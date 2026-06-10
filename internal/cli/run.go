package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
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

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
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
	token, ok, err := sec.Get(secrets.KeyBotToken)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no bot token stored — run `onibi setup` first")
	}
	logging.SetSecrets(token)

	owner, err := auth.LoadOwner(ctx, db)
	if err != nil {
		return err
	}

	logger := logging.New(cmd.ErrOrStderr(), slog.LevelInfo)

	bot, err := telegram.New(ctx, telegram.Options{Token: token})
	if err != nil {
		return err
	}

	d := daemon.New(daemon.Options{
		Paths:   paths,
		DB:      db,
		Secrets: sec,
		Owner:   owner,
		Bot:     bot,
		Log:     logger,
	})
	if bufSize > 0 {
		// per-session buffer size override is wired by SpawnAgent path; the
		// constant in daemon.go covers the default. (Real override would
		// thread through SpawnAgent; deferred to Phase 6 multi-session.)
		_ = bufSize
	}

	// optional agent spawn
	if len(args) > 0 {
		agent := args[0]
		rest := args[1:]
		bin, err := exec.LookPath(agent)
		if err != nil {
			return fmt.Errorf("agent %q not found in PATH: %w", agent, err)
		}
		s, err := d.SpawnAgent(ctx, name, agent, bin, rest, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "spawned %s (session %s)\n", agent, s.ID)

		// when agent exits, the read loop marks the session ended which
		// triggers daemon.Run to exit
		go func() {
			_ = s.Host.Wait()
			s.MarkEnded()
		}()
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "daemon-only mode (no agent spawned); waiting for hook events on", paths.Socket)
	}

	return d.Run(ctx)
}
