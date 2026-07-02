package daemon

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

const (
	autoUpdateCheckInterval = 24 * time.Hour
	autoUpdateCheckTimeout  = 2 * time.Second
)

func (d *Daemon) startAutoUpdateChecks(ctx context.Context, wg *sync.WaitGroup) {
	if !d.UpdateAuto {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runAutoUpdateChecks(ctx)
	}()
}

func (d *Daemon) runAutoUpdateChecks(ctx context.Context) {
	d.runAutoUpdateCheck(ctx)
	interval := d.UpdateCheckInterval
	if interval <= 0 {
		interval = autoUpdateCheckInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runAutoUpdateCheck(ctx)
		}
	}
}

func (d *Daemon) runAutoUpdateCheck(ctx context.Context) {
	if err := ctx.Err(); err != nil || d.UpdateCheck == nil {
		return
	}
	timeout := d.UpdateCheckTimeout
	if timeout <= 0 {
		timeout = autoUpdateCheckTimeout
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	exe, _ := os.Executable()
	res := d.UpdateCheck(checkCtx, updatecheck.Options{
		CurrentVersion: buildinfo.Version,
		CurrentCommit:  buildinfo.Commit,
		CheckGitHub:    true,
		Timeout:        timeout,
		Executable:     exe,
	})
	attrs := []any{
		slog.String("channel", d.UpdateChannel),
		slog.String("status", string(res.Status)),
		slog.String("source", string(res.Source)),
		slog.String("detail", res.Detail),
	}
	if res.LatestVersion != "" {
		attrs = append(attrs, slog.String("latest_version", res.LatestVersion))
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	log.Info("auto update check", attrs...)
}
