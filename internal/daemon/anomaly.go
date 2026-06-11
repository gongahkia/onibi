package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
)

type anomalyTracker struct {
	mu       sync.Mutex
	events   map[string][]time.Time
	lastWarn map[string]time.Time
}

func newAnomalyTracker() *anomalyTracker {
	return &anomalyTracker{
		events:   map[string][]time.Time{},
		lastWarn: map[string]time.Time{},
	}
}

func (a *anomalyTracker) Record(kind string, now time.Time) (string, bool) {
	if a == nil {
		return "", false
	}
	limit, window, ok := anomalyRule(kind)
	if !ok {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := now.Add(-window)
	kept := a.events[kind][:0]
	for _, t := range a.events[kind] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	a.events[kind] = kept
	if len(kept) < limit {
		return "", false
	}
	if last := a.lastWarn[kind]; !last.IsZero() && now.Sub(last) < 30*time.Minute {
		return "", false
	}
	a.lastWarn[kind] = now
	return fmt.Sprintf("Anomaly warning: %s reached %d events in %s. Run /status and onibi log.", kind, len(kept), window), true
}

func anomalyRule(kind string) (int, time.Duration, bool) {
	switch kind {
	case "approval.request":
		return 8, 5 * time.Minute, true
	case "approval.high_risk":
		return 3, 5 * time.Minute, true
	case "telegram.inject":
		return 10, 5 * time.Minute, true
	default:
		return 0, 0, false
	}
}

func (d *Daemon) noteAnomaly(ctx context.Context, kind string) {
	if d == nil || d.Bot == nil || d.Owner == nil {
		return
	}
	msg, warn := d.anomaly.Record(kind, time.Now())
	if !warn {
		return
	}
	sendMessage(ctx, d.Bot, &tgbot.SendMessageParams{ChatID: d.Owner.ID(), Text: msg})
}
