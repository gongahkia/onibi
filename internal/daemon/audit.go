package daemon

import (
	"context"
	"log/slog"
)

func (d *Daemon) audit(ctx context.Context, action, sessionID, payload string, decidedBy int64, detail string) {
	if d.DB == nil {
		return
	}
	if err := d.DB.AuditAppend(ctx, action, sessionID, payload, decidedBy, detail); err != nil {
		d.Log.Warn("audit append", slog.String("action", action), slog.Any("err", err))
	}
}
