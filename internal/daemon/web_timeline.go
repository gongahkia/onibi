package daemon

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/timeline"
)

func (d *Daemon) WebTimeline(ctx context.Context, limit int) ([]timeline.TimelineEvent, error) {
	if d == nil {
		return nil, errors.New("daemon unavailable")
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.webSessionRows(ctx)
	if err != nil {
		return nil, err
	}
	var out []timeline.TimelineEvent
	for _, row := range rows {
		if !strings.EqualFold(row.Agent, "claude") {
			continue
		}
		parser := d.Budget
		if parser == nil {
			parser = budget.NewClaudeParser("")
		}
		path, err := parser.FindTranscript(budget.SessionRef{SessionID: row.ID, Agent: row.Agent, CWD: row.CWD})
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && d.Log != nil {
				d.Log.Debug("timeline transcript lookup failed", "session", row.ID, "err", err)
			}
			continue
		}
		events, err := timeline.ParseFile(path, timeline.Options{SessionID: row.ID, Agent: row.Agent})
		if err != nil {
			if d.Log != nil {
				d.Log.Warn("timeline transcript parse failed", "session", row.ID, "path", path, "err", err)
			}
			continue
		}
		out = append(out, events...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TS != "" && out[j].TS != "" && out[i].TS != out[j].TS {
			return out[i].TS < out[j].TS
		}
		if out[i].SessionID != out[j].SessionID {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].Offset < out[j].Offset
	})
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
