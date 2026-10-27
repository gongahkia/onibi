package daemon

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// retiredUploadsDir held files staged by the removed Telegram upload handler.
// Nothing expires them any more, so the daemon deletes the directory on start.
const retiredUploadsDir = "uploads"

func (d *Daemon) removeRetiredUploads() {
	if d.Paths.StateDir == "" {
		return
	}
	if err := os.RemoveAll(filepath.Join(d.Paths.StateDir, retiredUploadsDir)); err != nil {
		d.Log.Warn("remove retired uploads", "err", err)
	}
}

func (d *Daemon) sweepExpiredControls(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.DB.KVPurgeExpired(ctx); err != nil {
				d.Log.Warn("purge expired controls", "err", err)
			}
		}
	}
}
