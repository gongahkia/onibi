package approval

import (
	"context"
	"log/slog"
	"time"
)

// SweepInterval controls how often the expiry sweeper runs. Coarse-grained
// — we don't need second-precision expiry; the 5-min hard limit + this
// interval means worst-case slack is 5m+SweepInterval (still well within
// the threat budget).
const SweepInterval = 15 * time.Second

// Sweeper runs ExpireOverdue on a ticker until ctx is cancelled.
type Sweeper struct {
	Queue    *Queue
	Interval time.Duration
	Log      *slog.Logger
}

// Run blocks. Safe to invoke as a daemon goroutine.
func (s *Sweeper) Run(ctx context.Context) {
	if s.Interval == 0 {
		s.Interval = SweepInterval
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := s.Queue.ExpireOverdue(ctx)
			if err != nil {
				s.Log.Warn("approval sweeper", slog.Any("err", err))
				continue
			}
			if n > 0 {
				s.Log.Info("approval expiry", slog.Int("expired", n))
			}
		}
	}
}
