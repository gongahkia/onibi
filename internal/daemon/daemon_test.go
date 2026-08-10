package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/courtsg/internal/app"
	"github.com/gongahkia/courtsg/internal/config"
)

func TestLockPreventsSecondDaemon(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir = t.TempDir()
	cfg.DatabasePath = filepath.Join(cfg.DataDir, "courtsg.db")
	service, err := app.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	runner, err := New(service, cfg.DataDir, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := acquireLock(runner.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if _, err := acquireLock(runner.lockPath); err == nil {
		t.Fatal("second daemon lock unexpectedly succeeded")
	}
}
