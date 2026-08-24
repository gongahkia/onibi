// Package perfectgym maps The Kallang's read-only PerfectGym calendar into
// Kaypoh's normalized badminton availability model.
package perfectgym

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/browser"
	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

const (
	SourceID               = "the-kallang"
	AccessMode             = "perfectgym"
	defaultFacilityType    = "Badminton Courts"
	defaultAvailabilityURL = "https://thekallang.perfectgym.com/clientportal2/"
	adapterVersion         = "perfectgym-browser-v1"
)

// Adapter keeps the provider-specific calendar interaction out of the generic
// partner JSON reader. The aggregated calendar is venue-level: it reports a
// bookable time but does not carry a stable court identity.
type Adapter struct {
	info           domain.SourceInfo
	settings       config.SourcePerfectGym
	refreshMinutes int
	scanner        browser.PerfectGymScanner

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(info domain.SourceInfo, settings config.Source, scanner browser.PerfectGymScanner) (*Adapter, error) {
	if info.ID != SourceID {
		return nil, fmt.Errorf("PerfectGym adapter source ID mismatch %q", info.ID)
	}
	if scanner == nil {
		return nil, errors.New("PerfectGym browser scanner is required")
	}
	if err := validateSettings(settings.PerfectGym); err != nil {
		return nil, err
	}
	return &Adapter{info: info, settings: settings.PerfectGym, refreshMinutes: settings.RefreshMinutes, scanner: scanner, health: domain.SourceHealth{SourceID: info.ID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

// DiscoverVenues is local-only because a successful calendar scan carries the
// single observed venue identity atomically with its normalized slots.
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
	fetchedAt := time.Now().UTC()
	entries, err := adapter.scanner.ScanPerfectGym(ctx, browser.PerfectGymScanRequest{
		AvailabilityURL: adapter.settings.AvailabilityURL, FacilityTypeName: facilityType(adapter.settings), SessionStateBase64: adapter.settings.SessionStateBase64,
		PermittedHosts: adapter.info.Policy.PermittedHosts, Timeout: adapter.info.Policy.Timeout, EndDate: request.EndDate,
	})
	if err != nil {
		adapter.setFailure(started, err)
		return source.AvailabilitySnapshot{}, err
	}
	venue := adapter.venue(fetchedAt)
	slots := make(map[string]domain.AvailabilitySlot)
	for _, entry := range entries {
		if !strings.EqualFold(entry.Status, "bookable") || !inRequestWindow(entry.Start, entry.End, request) {
			continue
		}
		slot := adapter.slot(venue, entry.Start, entry.End, fetchedAt)
		slots[slot.ID] = slot
	}
	result := make([]domain.AvailabilitySlot, 0, len(slots))
	for _, slot := range slots {
		result = append(result, slot)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Start.Equal(result[j].Start) {
			return result[i].End.Before(result[j].End)
		}
		return result[i].Start.Before(result[j].Start)
	})
	adapter.setSuccess(started, len(result))
	return source.AvailabilitySnapshot{Venues: []domain.Venue{venue}, Slots: result}, nil
}

func (adapter *Adapter) Health(context.Context) (domain.SourceHealth, error) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	return adapter.health, nil
}

func validateSettings(settings config.SourcePerfectGym) error {
	if !settings.Enabled {
		return errors.New("PerfectGym reader is not enabled")
	}
	parsed, err := url.Parse(settings.AvailabilityURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "thekallang.perfectgym.com" || parsed.Path != "/clientportal2/" {
		return fmt.Errorf("PerfectGym availability_url must be %s", defaultAvailabilityURL)
	}
	if strings.TrimSpace(settings.SessionStateBase64) == "" {
		return errors.New("PerfectGym reader needs an imported session_state_base64")
	}
	return nil
}

func facilityType(settings config.SourcePerfectGym) string {
	if name := strings.TrimSpace(settings.FacilityTypeName); name != "" {
		return name
	}
	return defaultFacilityType
}

func (adapter *Adapter) venue(fetchedAt time.Time) domain.Venue {
	return domain.Venue{
		ID: SourceID + ":venue:ocbc-arena", SourceIDs: []string{SourceID}, Name: "The Kallang / OCBC Arena",
		BookingURLs: []string{adapter.settings.AvailabilityURL}, Provenance: adapter.provenance("ocbc-arena", fetchedAt),
	}
}

func (adapter *Adapter) slot(venue domain.Venue, start, end, fetchedAt time.Time) domain.AvailabilitySlot {
	reference := strings.Join([]string{start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), venue.ID}, "\x00")
	return domain.AvailabilitySlot{
		ID: SourceID + ":slot:" + stableID(reference), VenueID: venue.ID, SourceID: SourceID, Start: start.UTC(), End: end.UTC(),
		Status: domain.AvailabilityAvailable, BookingURL: adapter.settings.AvailabilityURL, ObservedAt: fetchedAt, FetchedAt: fetchedAt,
		StaleAfter: adapter.staleAfter(fetchedAt), Provenance: adapter.provenance(reference, fetchedAt),
	}
}

func (adapter *Adapter) provenance(reference string, fetchedAt time.Time) domain.Provenance {
	return domain.Provenance{SourceID: SourceID, SourceReference: reference, ObservedAt: fetchedAt, FetchedAt: fetchedAt, AdapterVersion: adapterVersion, Confidence: 0.75}
}

func (adapter *Adapter) staleAfter(now time.Time) time.Time {
	minutes := adapter.refreshMinutes
	if minutes < 30 {
		minutes = 30
	}
	return now.Add(2 * time.Duration(minutes) * time.Minute)
}

func (adapter *Adapter) setSuccess(started time.Time, records int) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: SourceID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: AccessMode, LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: records}
}

func (adapter *Adapter) setFailure(started time.Time, err error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: SourceID, State: domain.HealthDegraded, LastAttempt: &now, LastCategory: "perfectgym_browser_failed", AccessFailures: []domain.AccessFailure{{Mode: AccessMode, Error: err.Error()}}, LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: adapter.health.ConsecutiveFailures + 1, LastError: err.Error()}
}

func inRequestWindow(start, end time.Time, request source.AvailabilityRequest) bool {
	if !request.StartDate.IsZero() && end.Before(request.StartDate) {
		return false
	}
	return request.EndDate.IsZero() || start.Before(request.EndDate)
}

func stableID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}
