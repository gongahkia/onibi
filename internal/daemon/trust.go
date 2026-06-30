package daemon

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/trust"
	"github.com/gongahkia/onibi/internal/web"
)

func (d *Daemon) startTrustWatcher(ctx context.Context, wg *sync.WaitGroup) error {
	if d == nil || d.Registry == nil {
		return nil
	}
	w, err := trust.NewWatcher(func(ev trust.WatchEvent) {
		d.handleTrustWatchEvent(ctx, ev)
	})
	if err != nil {
		return err
	}
	d.Trust = w
	d.syncTrustRoots(w)
	wg.Add(2)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.syncTrustRoots(w)
			}
		}
	}()
	return nil
}

func (d *Daemon) syncTrustRoots(w *trust.Watcher) {
	if w == nil || d.Registry == nil {
		return
	}
	for _, s := range d.Registry.List() {
		if s == nil || s.CWD == "" || s.Ended() {
			continue
		}
		if err := w.AddRoot(s.CWD); err != nil {
			d.audit(context.Background(), "trust.policy.watch_error", s.ID, "", 0, "root="+s.CWD+" err="+err.Error())
		}
	}
}

func (d *Daemon) handleTrustWatchEvent(ctx context.Context, ev trust.WatchEvent) {
	if ev.Err != nil {
		d.audit(ctx, "trust.policy.error", "", "", 0, fmt.Sprintf("root=%s path=%s err=%s", ev.Root, ev.Path, ev.Err))
		d.publishToast("Trust policy not reloaded: " + ev.Err.Error())
		return
	}
	if ev.Initial {
		return
	}
	d.audit(ctx, "trust.policy.reload", "", "", 0, fmt.Sprintf("root=%s path=%s rules=%d->%d changed=%t",
		ev.Root, ev.Path, len(ev.Previous.Rules), len(ev.Policy.Rules), !reflect.DeepEqual(ev.Previous, ev.Policy)))
}

func (d *Daemon) publishToast(message string) {
	if d == nil || d.Events == nil {
		return
	}
	d.Events.Publish(web.Event{
		Type: "toast",
		Payload: map[string]any{
			"level":   "warning",
			"message": message,
		},
	})
}
