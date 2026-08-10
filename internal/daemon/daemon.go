// Package daemon runs shared refresh, watch evaluation, and delivery retries.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gongahkia/kaypoh/internal/app"
	"github.com/gongahkia/kaypoh/internal/domain"
)

type Runner struct {
	service  *app.Service
	interval time.Duration
	lockPath string
}

type CycleReport struct {
	Refreshed  []app.RefreshResult           `json:"refreshed"`
	Watches    []app.WatchEvaluation         `json:"watches"`
	Deliveries []domain.NotificationDelivery `json:"deliveries"`
}

func New(service *app.Service, dataDir string, interval time.Duration) (*Runner, error) {
	if service == nil {
		return nil, errors.New("daemon service is required")
	}
	if interval < time.Minute {
		return nil, errors.New("daemon interval must be at least one minute")
	}
	if dataDir == "" {
		return nil, errors.New("daemon data directory is required")
	}
	return &Runner{service: service, interval: interval, lockPath: filepath.Join(dataDir, "kaypoh.lock")}, nil
}

// Run holds a local advisory lock for the process lifetime and exits cleanly
// when its context is cancelled. Each cycle refreshes a source at most once.
func (runner *Runner) Run(ctx context.Context) error {
	lock, err := acquireLock(runner.lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err := runner.Cycle(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(runner.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := runner.Cycle(ctx); err != nil {
				return err
			}
		}
	}
}

func (runner *Runner) RunOnce(ctx context.Context) (CycleReport, error) {
	lock, err := acquireLock(runner.lockPath)
	if err != nil {
		return CycleReport{}, err
	}
	defer lock.Close()
	return runner.Cycle(ctx)
}

func (runner *Runner) Cycle(ctx context.Context) (CycleReport, error) {
	report := CycleReport{}
	refreshed, err := runner.service.Refresh(ctx, nil)
	if err != nil {
		return report, err
	}
	report.Refreshed = refreshed
	watches, err := runner.service.EvaluateWatches(ctx)
	if err != nil {
		return report, err
	}
	report.Watches = watches
	deliveries, err := runner.service.RetryDeliveries(ctx, 5, 100)
	if err != nil {
		return report, err
	}
	report.Deliveries = deliveries
	return report, nil
}

type lockFile struct{ file *os.File }

func acquireLock(path string) (*lockFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create daemon lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another kaypoh daemon is already running: %w", err)
	}
	return &lockFile{file: file}, nil
}

func (lock *lockFile) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN); err != nil {
		lock.file.Close()
		return err
	}
	return lock.file.Close()
}
