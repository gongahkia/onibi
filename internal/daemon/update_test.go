package daemon

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

func TestAutoUpdateDefaultsOptOut(t *testing.T) {
	var calls atomic.Int32
	d := New(Options{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		UpdateCheck: func(context.Context, updatecheck.Options) updatecheck.Result {
			calls.Add(1)
			return updatecheck.Result{}
		},
	})
	if d.UpdateAuto {
		t.Fatal("auto update defaulted on")
	}
	if d.UpdateChannel != "stable" {
		t.Fatalf("channel = %q", d.UpdateChannel)
	}
	if d.UpdateCheckInterval != 24*time.Hour {
		t.Fatalf("interval = %s", d.UpdateCheckInterval)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	d.startAutoUpdateChecks(ctx, &wg)
	cancel()
	wg.Wait()
	if calls.Load() != 0 {
		t.Fatalf("disabled auto update made %d checks", calls.Load())
	}
}

func TestAutoUpdateChecksStartupAndInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan updatecheck.Options, 4)
	var count atomic.Int32
	d := New(Options{
		Log:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		UpdateAuto:          true,
		UpdateChannel:       "stable",
		UpdateCheckInterval: 10 * time.Millisecond,
		UpdateCheckTimeout:  time.Second,
		UpdateCheck: func(_ context.Context, opts updatecheck.Options) updatecheck.Result {
			calls <- opts
			if count.Add(1) == 2 {
				cancel()
			}
			return updatecheck.Result{Status: updatecheck.StatusCurrent, Source: updatecheck.SourceGitHub, Detail: "current"}
		},
	})
	var wg sync.WaitGroup
	d.startAutoUpdateChecks(ctx, &wg)
	first := readUpdateCall(t, calls)
	second := readUpdateCall(t, calls)
	for _, got := range []updatecheck.Options{first, second} {
		if got.CurrentVersion != buildinfo.Version || got.CurrentCommit != buildinfo.Commit || !got.CheckGitHub || got.Timeout != time.Second {
			t.Fatalf("options = %#v", got)
		}
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("auto update checker did not stop")
	}
}

func readUpdateCall(t *testing.T, calls <-chan updatecheck.Options) updatecheck.Options {
	t.Helper()
	select {
	case opts := <-calls:
		return opts
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update check")
		return updatecheck.Options{}
	}
}
