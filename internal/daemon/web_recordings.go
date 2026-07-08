package daemon

import (
	"context"
	"os"

	"github.com/gongahkia/onibi/internal/web"
)

func (d *Daemon) WebRecordings(ctx context.Context) ([]web.RecordingSummary, error) {
	if d == nil || d.Recorder == nil {
		return nil, nil
	}
	return d.Recorder.List(ctx)
}

func (d *Daemon) WebRecordingPath(ctx context.Context, id string) (string, bool, error) {
	_ = ctx
	if d == nil || d.Recorder == nil {
		return "", false, nil
	}
	path := d.Recorder.Path(id)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if info.IsDir() {
		return "", false, nil
	}
	return path, true, nil
}
