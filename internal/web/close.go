package web

import (
	"context"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
)

// A successful close Apple event is an acknowledgement, not proof that the GUI
// removed its panes. Observe at most three times under one bounded budget.
// This never retries the mutation, including when Ghostty shows a local prompt.
func (s *Server) observeClose(ctx context.Context, action ghostty.Action, result *ghostty.Result) {
	ctx, cancel := context.WithTimeout(ctx, s.closeObservationBudget)
	defer cancel()
	result.CloseState = "unverified"
	result.Message = "Close was requested, but its outcome could not be checked. Inspect your Mac before trying again; nothing will be resent automatically."
	expected := make(map[string]bool, len(action.ExpectedTargets))
	for _, id := range action.ExpectedTargets {
		expected[id] = true
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		started := s.now()
		targets, err := s.backend.Targets(ctx)
		s.recordInventory(started, err)
		if err != nil {
			result.CloseState, result.ObservedAt = "unverified", time.Time{}
			result.Message = "Close was requested, but its outcome could not be checked. Inspect your Mac before trying again; nothing will be resent automatically."
			return
		}
		remaining := len(ghostty.CloseTargets(action, targets)) > 0
		for _, t := range targets {
			remaining = remaining || expected[t.ID]
		}
		result.ObservedAt = s.now().UTC()
		if !remaining {
			result.CloseState = "removed"
			result.Message = "The reviewed panes are no longer available."
			return
		}
		result.CloseState = "still_present"
		result.Message = "Panes are still available after the close request. Check for a confirmation on your Mac; nothing will be resent automatically."
	}
}
