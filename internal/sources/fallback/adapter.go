// Package fallback composes independently approved availability readers into
// one ordered, observable source access chain.
package fallback

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

// Attempt is one named, read-only way to obtain a complete availability
// snapshot. Attempts run in the supplied order and stop at the first success.
type Attempt struct {
	Mode  string
	Fetch func(context.Context, source.AvailabilityRequest) (source.AvailabilitySnapshot, error)
}

type Adapter struct {
	info     domain.SourceInfo
	attempts []Attempt

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(info domain.SourceInfo, attempts []Attempt) (*Adapter, error) {
	if info.ID == "" {
		return nil, errors.New("source ID is required")
	}
	if len(attempts) == 0 {
		return nil, errors.New("at least one fallback attempt is required")
	}
	seen := make(map[string]struct{}, len(attempts))
	for _, attempt := range attempts {
		if attempt.Mode == "" || attempt.Fetch == nil {
			return nil, errors.New("fallback attempts need a mode and fetch function")
		}
		if _, ok := seen[attempt.Mode]; ok {
			return nil, fmt.Errorf("duplicate fallback attempt %q", attempt.Mode)
		}
		seen[attempt.Mode] = struct{}{}
	}
	return &Adapter{info: info, attempts: append([]Attempt(nil), attempts...), health: domain.SourceHealth{SourceID: info.ID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

// DiscoverVenues is local-only because every successful snapshot carries its
// observed venue identity, which the application persists atomically.
func (adapter *Adapter) DiscoverVenues(context.Context) ([]domain.Venue, error) { return nil, nil }

func (adapter *Adapter) FetchAvailability(ctx context.Context, request source.AvailabilityRequest) ([]domain.AvailabilitySlot, error) {
	snapshot, err := adapter.FetchSnapshot(ctx, request)
	if err != nil {
		return nil, err
	}
	return snapshot.Slots, nil
}

func (adapter *Adapter) FetchSnapshot(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	started := time.Now()
	var (
		failures       []error
		accessFailures []domain.AccessFailure
	)
	for _, attempt := range adapter.attempts {
		snapshot, err := attempt.Fetch(ctx, request)
		if err == nil {
			adapter.setSuccess(started, len(snapshot.Slots), attempt.Mode, accessFailures)
			return snapshot, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", attempt.Mode, err))
		accessFailures = append(accessFailures, domain.AccessFailure{Mode: attempt.Mode, Error: err.Error()})
	}
	err := errors.Join(failures...)
	adapter.setFailure(started, err, accessFailures)
	return source.AvailabilitySnapshot{}, err
}

func (adapter *Adapter) Health(context.Context) (domain.SourceHealth, error) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	return adapter.health, nil
}

func (adapter *Adapter) setSuccess(started time.Time, records int, mode string, accessFailures []domain.AccessFailure) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: mode, AccessFailures: accessFailures, LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: records}
}

func (adapter *Adapter) setFailure(started time.Time, err error, accessFailures []domain.AccessFailure) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: domain.HealthDegraded, LastAttempt: &now, LastCategory: "all_access_modes_failed", AccessFailures: accessFailures, LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: adapter.health.ConsecutiveFailures + 1, LastError: err.Error()}
}
