package daemon

import (
	"context"
	"time"
)

// IdleThreshold is how long a session must be silent after recent activity
// before we consider its turn complete. Picked to be longer than the
// "thinking pause" of typical agents but short enough to feel responsive.
const IdleThreshold = 3 * time.Second

// IdleDetector polls the registry and fires fn(session) for each session
// that has been silent for at least IdleThreshold. Each session is fired
// at most once per "active period" — after firing, the detector waits for
// the next Touch before considering that session again.
//
// Design note: this is the FALLBACK path for turn-complete detection.
// The PRIMARY path is the agent's Stop hook → onibi-notify → intake →
// daemon. The fallback fires later (longer threshold) so it only kicks in
// when the agent has no hook installed (Phase 8 will reduce reliance on
// this for hooked agents).
type IdleDetector struct {
	Registry  *Registry
	Threshold time.Duration
	Interval  time.Duration
	OnIdle    func(s *Session)

	fired map[string]time.Time // session id → last fire time (to dedup)
}

// Run blocks until ctx is cancelled, polling on Interval ticks.
func (d *IdleDetector) Run(ctx context.Context) {
	if d.Threshold == 0 {
		d.Threshold = IdleThreshold
	}
	if d.Interval == 0 {
		d.Interval = 500 * time.Millisecond
	}
	if d.fired == nil {
		d.fired = map[string]time.Time{}
	}

	t := time.NewTicker(d.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			d.tick(now)
		}
	}
}

func (d *IdleDetector) tick(_ time.Time) {
	for _, s := range d.Registry.List() {
		if s.Ended() {
			continue
		}
		idle := s.SinceActivity()
		if idle < d.Threshold {
			// session is active again — clear fired marker so the next
			// idle period can refire
			delete(d.fired, s.ID)
			continue
		}
		// idle long enough; fire if we haven't already since the most
		// recent activity
		last := d.fired[s.ID]
		if !last.IsZero() && time.Since(last) < d.Threshold {
			continue
		}
		d.fired[s.ID] = time.Now()
		if d.OnIdle != nil {
			d.OnIdle(s)
		}
	}
}
