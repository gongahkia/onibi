package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/spf13/cobra"
)

// doctorCmd checks the prerequisites and local configuration needed to run
// Onibi. It deliberately avoids creating state or starting services, so it is
// safe to run before first-time setup.
func doctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Check local setup and readiness",
		Args:          cobra.NoArgs,
		RunE:          runDoctor,
		SilenceErrors: true,
	}
	cmd.Flags().Bool("check-telegram", false, "validate the configured bot token with Telegram")
	return cmd
}

type doctorLevel string

const (
	doctorOK   doctorLevel = "OK"
	doctorInfo doctorLevel = "INFO"
	doctorFail doctorLevel = "FAIL"
)

type doctorCheck struct {
	Level  doctorLevel
	Name   string
	Detail string
}

type doctorReport struct {
	checks []doctorCheck
}

func (r *doctorReport) add(level doctorLevel, name, detail string) {
	r.checks = append(r.checks, doctorCheck{Level: level, Name: name, Detail: detail})
}

func (r doctorReport) failures() int {
	count := 0
	for _, check := range r.checks {
		if check.Level == doctorFail {
			count++
		}
	}
	return count
}

func (r doctorReport) writeTo(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), "Onibi doctor")
	for _, check := range r.checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-18s %s\n", check.Level, check.Name+":", check.Detail)
	}
	if failed := r.failures(); failed > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nNot ready: %d required check(s) failed.\n", failed)
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nReady to start. Optional integrations are reported as INFO.")
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(contextOrBackground(cmd.Context()), 12*time.Second)
	defer cancel()
	checkTelegram, _ := cmd.Flags().GetBool("check-telegram")
	report := doctorReport{}

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		report.add(doctorOK, "platform", runtime.GOOS)
	} else {
		report.add(doctorFail, "platform", fmt.Sprintf("%s is unsupported; use Linux or macOS", runtime.GOOS))
	}

	if path, err := exec.LookPath("tmux"); err == nil {
		report.add(doctorOK, "tmux", path)
	} else {
		report.add(doctorFail, "tmux", "not found in PATH; install tmux")
	}

	if path, err := os.Executable(); err == nil {
		report.add(doctorOK, "executable", path)
	} else {
		report.add(doctorFail, "executable", err.Error())
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		report.add(doctorFail, "paths", err.Error())
		report.writeTo(cmd)
		return fmt.Errorf("doctor found %d required check(s) failed", report.failures())
	}
	doctorCheckState(&report, paths)

	cfg, meta, err := config.Load(paths)
	if err != nil {
		report.add(doctorFail, "config", err.Error())
	} else if err := render.ValidateFont(cfg.Screen.Font, cfg.Screen.FontPath); err != nil {
		report.add(doctorFail, "screen font", err.Error())
	} else if meta.Exists {
		report.add(doctorOK, "config", paths.Config)
	} else {
		report.add(doctorOK, "config", "defaults ("+paths.Config+" will be created when configured)")
	}

	token, tokenErr := doctorTelegramToken(ctx, paths)
	if tokenErr != nil {
		report.add(doctorFail, "Telegram token", tokenErr.Error())
	} else if token == "" {
		report.add(doctorFail, "Telegram token", "missing; run onibi telegram setup --token <BotFather token>")
	} else if !telegram.ValidBotToken(token) {
		report.add(doctorFail, "Telegram token", "does not look like a BotFather token")
	} else {
		report.add(doctorOK, "Telegram token", "configured")
	}
	if checkTelegram {
		doctorCheckTelegram(ctx, &report, token, tokenErr)
	} else {
		report.add(doctorInfo, "Telegram API", "not checked; rerun with --check-telegram")
	}

	doctorCheckProgram(&report, "codex", "optional; required for Codex sessions")
	doctorCheckProgram(&report, "claude", "optional; required for Claude Code sessions")
	doctorCheckProgram(&report, "pi", "optional; required for Pi sessions")
	doctorCheckDaemon(&report, paths)
	doctorCheckService(ctx, &report)

	report.writeTo(cmd)
	if failed := report.failures(); failed > 0 {
		return fmt.Errorf("doctor found %d required check(s) failed", failed)
	}
	return nil
}

func doctorCheckState(report *doctorReport, paths config.Paths) {
	if err := doctorCheckPrivateDirectory(paths.StateDir); os.IsNotExist(err) {
		report.add(doctorOK, "state directory", paths.StateDir+" (will be created during setup)")
		return
	} else if err != nil {
		report.add(doctorFail, "state directory", err.Error())
		return
	}
	if err := paths.EnvFilePerms(); err != nil {
		report.add(doctorFail, "secret fallback", err.Error())
		return
	}
	report.add(doctorOK, "state directory", paths.StateDir)
	if paths.LogDir == "" {
		return
	}
	if err := doctorCheckPrivateDirectory(paths.LogDir); os.IsNotExist(err) {
		report.add(doctorOK, "log directory", paths.LogDir+" (will be created on start)")
	} else if err != nil {
		report.add(doctorFail, "log directory", err.Error())
	} else {
		report.add(doctorOK, "log directory", paths.LogDir)
	}
}

func doctorCheckPrivateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if info.Mode().Perm()&^os.FileMode(0o700) != 0 {
		return fmt.Errorf("permissions %#o are too broad; run chmod 700 %s", info.Mode().Perm(), path)
	}
	return nil
}

func doctorTelegramToken(ctx context.Context, paths config.Paths) (string, error) {
	if value := strings.TrimSpace(os.Getenv("ONIBI_TELEGRAM_TOKEN")); value != "" {
		return value, nil
	}
	store, err := telegramSecrets(paths)
	if err != nil {
		return "", err
	}
	value, ok, err := store.GetWithTimeout(ctx, daemon.TelegramSecretBotToken, 3*time.Second)
	if err != nil || !ok {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func doctorCheckTelegram(ctx context.Context, report *doctorReport, token string, tokenErr error) {
	if tokenErr != nil || !telegram.ValidBotToken(token) {
		report.add(doctorInfo, "Telegram API", "skipped because no valid token is available")
		return
	}
	bot, err := telegram.NewClient(token).GetMe(ctx)
	if err != nil {
		report.add(doctorFail, "Telegram API", err.Error())
		return
	}
	report.add(doctorOK, "Telegram API", "connected to @"+bot.Username)
}

func doctorCheckProgram(report *doctorReport, program, missingDetail string) {
	path, err := exec.LookPath(program)
	if err != nil {
		report.add(doctorInfo, program, missingDetail)
		return
	}
	report.add(doctorInfo, program, "available at "+path)
}

func doctorCheckDaemon(report *doctorReport, paths config.Paths) {
	if _, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypePing}, 750*time.Millisecond); err != nil {
		report.add(doctorInfo, "daemon", "not running; run onibi start")
		return
	}
	report.add(doctorOK, "daemon", "responding on "+paths.Socket)
}

func doctorCheckService(ctx context.Context, report *doctorReport) {
	m, err := manager()
	if err != nil {
		report.add(doctorInfo, "service", "unavailable: "+err.Error())
		return
	}
	status := m.Status(ctx)
	if !status.Installed {
		report.add(doctorInfo, "service", "not installed; use onibi system service install after a foreground test")
		return
	}
	if status.Running {
		report.add(doctorOK, "service", "running")
		return
	}
	detail := strings.TrimSpace(status.Detail)
	if detail == "" {
		detail = "installed but not running"
	} else {
		detail = "installed but not running (" + detail + ")"
	}
	report.add(doctorInfo, "service", detail)
}
