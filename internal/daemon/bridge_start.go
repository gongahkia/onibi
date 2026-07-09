//go:build !onibi_remote

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/gongahkia/onibi/internal/apns"
	"github.com/gongahkia/onibi/internal/discord"
	emailapi "github.com/gongahkia/onibi/internal/email"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pushover"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/sms"
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

func (d *Daemon) startGotifyNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if strings.TrimSpace(d.Gotify.AppToken) == "" {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runGotifyNotifier(ctx, gotify.New(d.Gotify.BaseURL, d.Gotify.AppToken, d.Gotify.ClientToken))
	}()
}

func (d *Daemon) startAPNsNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if !d.apnsConfigured() {
		return
	}
	c, err := apns.New(apns.Config{
		KeyPath:     d.APNs.KeyPath,
		KeyID:       d.APNs.KeyID,
		TeamID:      d.APNs.TeamID,
		Topic:       d.APNs.Topic,
		Environment: d.APNs.Environment,
	})
	if err != nil {
		d.Log.Error("apns notifier", slog.Any("err", err))
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runAPNsNotifier(ctx, c)
	}()
}

func (d *Daemon) startSMSNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if !d.smsConfigured() {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runSMSNotifier(ctx, sms.New(d.SMS.AccountSID, d.SMS.AuthToken, d.SMS.From, d.SMS.MessagingServiceSID))
	}()
}

func (d *Daemon) startEmailNotifier(ctx context.Context, wg *sync.WaitGroup) {
	if !d.emailConfigured() {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runEmailNotifier(ctx, emailapi.New(d.Email.Addr, d.Email.Host, d.Email.Username, d.Email.Password, d.Email.From))
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

func (d *Daemon) apnsConfigured() bool {
	return strings.TrimSpace(d.APNs.KeyPath) != "" &&
		strings.TrimSpace(d.APNs.KeyID) != "" &&
		strings.TrimSpace(d.APNs.TeamID) != "" &&
		strings.TrimSpace(d.APNs.Topic) != "" &&
		strings.TrimSpace(d.APNs.DeviceToken) != ""
}

func (d *Daemon) smsConfigured() bool {
	return strings.TrimSpace(d.SMS.AccountSID) != "" &&
		strings.TrimSpace(d.SMS.AuthToken) != "" &&
		strings.TrimSpace(d.SMS.To) != "" &&
		strings.TrimSpace(d.SMS.ActionBaseURL) != "" &&
		(strings.TrimSpace(d.SMS.From) != "" || strings.TrimSpace(d.SMS.MessagingServiceSID) != "")
}

func (d *Daemon) emailConfigured() bool {
	return strings.TrimSpace(d.Email.Addr) != "" &&
		strings.TrimSpace(d.Email.From) != "" &&
		strings.TrimSpace(d.Email.To) != "" &&
		strings.TrimSpace(d.Email.ActionBaseURL) != ""
}
