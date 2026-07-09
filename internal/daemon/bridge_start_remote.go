//go:build onibi_remote

package daemon

import (
	"context"
	"sync"
)

func (d *Daemon) startTelegramBridge(context.Context, *sync.WaitGroup, context.CancelFunc) {}
func (d *Daemon) startMatrixBridge(context.Context, *sync.WaitGroup, context.CancelFunc)   {}
func (d *Daemon) startSlackBridge(context.Context, *sync.WaitGroup, context.CancelFunc)    {}
func (d *Daemon) startDiscordBridge(context.Context, *sync.WaitGroup, context.CancelFunc)  {}
func (d *Daemon) startZulipBridge(context.Context, *sync.WaitGroup, context.CancelFunc)    {}
func (d *Daemon) startIRCBridge(context.Context, *sync.WaitGroup, context.CancelFunc)      {}
func (d *Daemon) startPushoverNotifier(context.Context, *sync.WaitGroup)                   {}
func (d *Daemon) startNtfyNotifier(context.Context, *sync.WaitGroup)                       {}
func (d *Daemon) startGotifyNotifier(context.Context, *sync.WaitGroup)                     {}
func (d *Daemon) startAPNsNotifier(context.Context, *sync.WaitGroup)                       {}
func (d *Daemon) startSMSNotifier(context.Context, *sync.WaitGroup)                        {}
func (d *Daemon) startEmailNotifier(context.Context, *sync.WaitGroup)                      {}
func (d *Daemon) startWebPushNotifier(context.Context, *sync.WaitGroup)                    {}
func (d *Daemon) apnsConfigured() bool                                                     { return false }
func (d *Daemon) smsConfigured() bool                                                      { return false }
func (d *Daemon) emailConfigured() bool                                                    { return false }
