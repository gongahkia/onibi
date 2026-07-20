//go:build !onibi_remote

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/gongahkia/onibi/internal/matrix"
)

func (d *Daemon) startTelegramBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.TelegramToken) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runTelegramBridge(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("telegram bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startMatrixBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.Matrix.AccessToken) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runMatrixBridge(ctx, matrix.New(d.Matrix.Homeserver, d.Matrix.AccessToken)); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("matrix bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startWebPushNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if d.DB == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runWebPushNotifier(ctx)
	}()
}
