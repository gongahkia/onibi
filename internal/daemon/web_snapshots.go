package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/web"
)

func (d *Daemon) WebSnapshots(ctx context.Context) ([]web.Snapshot, error) {
	if d == nil || d.DB == nil {
		return nil, errors.New("snapshot store unavailable")
	}
	rows, err := d.DB.SnapshotsList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]web.Snapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, web.Snapshot{
			Name:             row.Name,
			SessionID:        row.SessionID,
			CreatedAt:        row.CreatedAt.Format(time.RFC3339),
			CWD:              row.CWD,
			TranscriptOffset: row.TranscriptOffset,
		})
	}
	return out, nil
}

func (d *Daemon) WebRestoreSnapshot(ctx context.Context, name string) (web.SnapshotActionResult, error) {
	resp, err := d.handleSnapshotRPC(ctx, intake.Event{Type: intake.TypeSnapshot, SnapshotAction: "restore", SnapshotName: name})
	if err != nil {
		return web.SnapshotActionResult{}, err
	}
	return web.SnapshotActionResult{SessionID: resp.SessionID, Message: resp.Text}, nil
}

func (d *Daemon) WebForkSnapshot(ctx context.Context, req web.SnapshotForkRequest) (web.SnapshotActionResult, error) {
	resp, err := d.handleSnapshotRPC(ctx, intake.Event{
		Type:           intake.TypeSnapshot,
		SnapshotAction: "fork",
		SnapshotName:   req.Name,
		SnapshotTurn:   req.Turn,
		Text:           req.NewPrompt,
	})
	if err != nil {
		return web.SnapshotActionResult{}, err
	}
	return web.SnapshotActionResult{SessionID: resp.SessionID, Message: resp.Text}, nil
}
