package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
)

// Observations describe completed work, not a promise that the next action will
// succeed. Inventory and capture are independent capabilities.
type Observation struct {
	State      string    `json:"state"`
	Code       string    `json:"code,omitempty"`
	CheckedAt  time.Time `json:"checkedAt"`
	DurationMS int64     `json:"durationMs"`
	TargetID   string    `json:"targetId,omitempty"`
}

type inventoryFlight struct {
	done     chan struct{}
	cancel   context.CancelFunc
	refs     int
	finished bool
	targets  []ghostty.Target
	err      error
}

func (s *Server) capabilities() ghostty.Capabilities {
	if provider, ok := s.backend.(interface{ Capabilities() ghostty.Capabilities }); ok {
		return provider.Capabilities()
	}
	return ghostty.CapabilitiesForVersion("")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	diagnostics := ghostty.Diagnostics{CheckedAt: s.now(), Capabilities: s.capabilities(), Checks: []ghostty.Check{{ID: "installation", State: "unknown", Detail: "Passive diagnostics are unavailable for this backend."}}}
	if s.diagnostics != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		diagnostics = s.diagnostics(ctx)
	}
	s.mu.Lock()
	inventory, capture, retry := s.inventoryObservation, s.captureObservation, s.inventoryRetryAt
	s.mu.Unlock()
	if inventory.State == "" {
		inventory.State = "unknown"
	}
	if capture.State == "" {
		capture.State = "unknown"
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diagnostics, "inventory": inventory, "capture": capture, "retryAt": retry, "origin": s.origin})
}

func (s *Server) recordInventory(start time.Time, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.mu.Lock()
	if start.Before(s.inventoryStarted) {
		s.mu.Unlock()
		return
	}
	s.inventoryStarted = start
	observation := Observation{State: "ready", CheckedAt: s.now(), DurationMS: max(0, s.now().Sub(start).Milliseconds())}
	if err != nil {
		observation.State = "unavailable"
		observation.Code = ghostty.ErrorCode(err)
		s.inventoryFailures = min(s.inventoryFailures+1, 5)
		delay := min(5*time.Second*time.Duration(1<<(s.inventoryFailures-1)), 60*time.Second)
		s.inventoryRetryAt = s.now().Add(delay)
		s.inventoryErr = err
	} else {
		s.inventoryFailures = 0
		s.inventoryRetryAt = time.Time{}
		s.inventoryErr = nil
	}
	previous := s.inventoryObservation
	s.inventoryObservation = observation
	s.mu.Unlock()
	if previous.State != observation.State || previous.Code != observation.Code {
		s.log.Info("web.native_state", "state", observation.State, "code", observation.Code, "duration_ms", observation.DurationMS)
	}
}

func (s *Server) recordCapture(start time.Time, id string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.mu.Lock()
	if start.Before(s.captureStarted) {
		s.mu.Unlock()
		return
	}
	s.captureStarted = start
	observation := Observation{State: "ready", TargetID: id, CheckedAt: s.now(), DurationMS: max(0, s.now().Sub(start).Milliseconds())}
	if err != nil {
		observation.State = "unavailable"
		observation.Code = ghostty.ErrorCode(err)
	}
	previous := s.captureObservation
	s.captureObservation = observation
	s.mu.Unlock()
	if previous.State != observation.State || previous.Code != observation.Code {
		s.log.Info("web.capture_state", "state", observation.State, "code", observation.Code, "duration_ms", observation.DurationMS)
	}
}

// Inventory polls share only in-flight work. Execution preconditions always
// bypass this path and read fresh native state. The last disconnected waiter
// cancels the shared read; a new client does not inherit a canceled flight.
func (s *Server) observedTargets(ctx context.Context) ([]ghostty.Target, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.inventoryErr != nil && s.now().Before(s.inventoryRetryAt) {
		err := s.inventoryErr
		s.mu.Unlock()
		return nil, err
	}
	f := s.inventoryFlight
	if f == nil {
		nativeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		f = &inventoryFlight{done: make(chan struct{}), cancel: cancel}
		s.inventoryFlight = f
		go func() {
			start := s.now()
			targets, err := s.backend.Targets(nativeCtx)
			if !errors.Is(nativeCtx.Err(), context.Canceled) {
				s.recordInventory(start, err)
			}
			s.mu.Lock()
			f.targets, f.err, f.finished = targets, err, true
			if s.inventoryFlight == f {
				s.inventoryFlight = nil
			}
			close(f.done)
			s.mu.Unlock()
			cancel()
		}()
	}
	f.refs++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		f.refs--
		if f.refs == 0 && !f.finished {
			f.cancel()
			if s.inventoryFlight == f {
				s.inventoryFlight = nil
			}
		}
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return append([]ghostty.Target(nil), f.targets...), f.err
	}
}

func (s *Server) writeRetryAfter(w http.ResponseWriter) {
	s.mu.Lock()
	delay := s.inventoryRetryAt.Sub(s.now())
	s.mu.Unlock()
	if delay > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int((delay+time.Second-1)/time.Second))))
	}
}
