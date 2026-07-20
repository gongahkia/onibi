//go:build !onibi_remote

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pushover"
	signalapi "github.com/gongahkia/onibi/internal/signal"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/zulip"
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

func (d *Daemon) startSlackBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.Slack.AppToken) == "" && strings.TrimSpace(d.Slack.BotToken) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runSlackBridge(ctx, slack.New(d.Slack.AppToken, d.Slack.BotToken)); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("slack bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startDiscordBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.Discord.Token) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runDiscordBridge(ctx, discord.New(d.Discord.Token)); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("discord bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startZulipBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.Zulip.APIKey) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runZulipBridge(ctx, zulip.New(d.Zulip.BaseURL, d.Zulip.Email, d.Zulip.APIKey)); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("zulip bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startIRCBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.IRC.Nick) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		c := irc.New(d.IRC.Addr, d.IRC.Nick, d.IRC.Username, d.IRC.Password)
		c.Plaintext = d.IRC.Plaintext
		if err := d.runIRCBridge(ctx, c); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("irc bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startSignalBridge(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) {
	if strings.TrimSpace(d.Signal.RPCURL) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.runSignalBridge(ctx, signalapi.New(d.Signal.RPCURL, d.Signal.Account)); err != nil && !errors.Is(err, context.Canceled) {
			d.Log.Error("signal bridge", slog.Any("err", err))
			cancel()
		}
	}()
}

func (d *Daemon) startPushoverNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if strings.TrimSpace(d.Pushover.Token) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runPushoverNotifier(ctx, pushover.New(d.Pushover.Token, d.Pushover.UserKey))
	}()
}

func (d *Daemon) startNtfyNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if strings.TrimSpace(d.Ntfy.Topic) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runNtfyNotifier(ctx, ntfy.New(d.Ntfy.BaseURL, d.Ntfy.Topic, d.Ntfy.Token))
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
