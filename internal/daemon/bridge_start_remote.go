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
func (d *Daemon) startSignalBridge(context.Context, *sync.WaitGroup, context.CancelFunc)   {}
func (d *Daemon) startWebPushNotifier(context.Context, *sync.WaitGroup)                    {}
