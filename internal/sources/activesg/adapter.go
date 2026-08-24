// Package activesg maps the read-only ActiveSG badminton browser surface into
// Kaypoh's normalized, bookable slot model.
package activesg

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
	SourceID              = "myactivesg"
	AccessMode            = "activesg"
	BadmintonVenueListURL = "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"
	adapterVersion        = "activesg-browser-v1"
)

// Adapter keeps ActiveSG-specific UI interpretation isolated from generic
// partner API and JSON readers. Only instant, hourly slots enter Kaypoh's
// bookable search model; ballot-state rows remain observed but are not slots.
type Adapter struct {
	info           domain.SourceInfo
	settings       config.SourceActiveSG
	refreshMinutes int
	scanner        browser.ActiveSGScanner

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(info domain.SourceInfo, settings config.Source, scanner browser.ActiveSGScanner) (*Adapter, error) {
	if info.ID != SourceID {
		return nil, fmt.Errorf("ActiveSG adapter source ID mismatch %q", info.ID)
	}
	if scanner == nil {
		return nil, errors.New("ActiveSG browser scanner is required")
	}
	if err := validateSettings(settings.ActiveSG); err != nil {
		return nil, err
	}
	return &Adapter{info: info, settings: settings.ActiveSG, refreshMinutes: settings.RefreshMinutes, scanner: scanner, health: domain.SourceHealth{SourceID: info.ID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

// DiscoverVenues is intentionally local-only. The same successful scan that
// observes slots returns the selected venue identities atomically.
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
	now := time.Now().UTC()
	rows, err := adapter.scanner.ScanActiveSG(ctx, browser.ActiveSGScanRequest{
		VenueListURL: adapter.settings.VenueListURL, VenueNames: adapter.settings.VenueNames, ScanAll: adapter.settings.ScanAll,
		SessionStateBase64: adapter.settings.SessionStateBase64, PermittedHosts: adapter.info.Policy.PermittedHosts, Timeout: adapter.info.Policy.Timeout,
	})
	if err != nil {
		adapter.setFailure(started, err, domain.HealthDegraded, "browser_scan_failed")
		return source.AvailabilitySnapshot{}, err
	}
	snapshot, err := adapter.normalize(rows, request, now)
	if err != nil {
		adapter.setFailure(started, err, domain.HealthDegraded, "invalid_browser_scan")
		return source.AvailabilitySnapshot{}, err
	}
	adapter.setSuccess(started, len(snapshot.Slots))
	return snapshot, nil
}

func (adapter *Adapter) Health(context.Context) (domain.SourceHealth, error) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	return adapter.health, nil
}

func validateSettings(settings config.SourceActiveSG) error {
	if !settings.Enabled {
		return errors.New("ActiveSG reader is not enabled")
	}
	parsed, err := url.Parse(settings.VenueListURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "activesg.gov.sg" || parsed.Path != "/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues" {
		return fmt.Errorf("ActiveSG venue_list_url must be %s", BadmintonVenueListURL)
	}
	if strings.TrimSpace(settings.SessionStateBase64) == "" {
		return errors.New("ActiveSG reader needs an imported session_state_base64")
	}
	if !settings.ScanAll && len(normalizedNames(settings.VenueNames)) == 0 {
		return errors.New("ActiveSG reader needs venue_names or scan_all = true")
	}
	return nil
}

func (adapter *Adapter) normalize(rows []browser.ActiveSGAvailability, request source.AvailabilityRequest, fetchedAt time.Time) (source.AvailabilitySnapshot, error) {
	venues := make(map[string]domain.Venue)
	slots := make(map[string]domain.AvailabilitySlot)
	for index, row := range rows {
		if err := validateRow(row); err != nil {
			return source.AvailabilitySnapshot{}, fmt.Errorf("normalize ActiveSG row %d: %w", index+1, err)
		}
		venue := adapter.venue(row, fetchedAt)
		venues[venue.ID] = venue
		if row.AvailabilityType != browser.ActiveSGInstant || row.AvailabilityStatus != browser.ActiveSGSlotsVisible {
			continue
		}
		for _, value := range row.SlotStartTimes {
			start, err := activeSGStart(row.Date, value)
			if err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("normalize ActiveSG row %d slot %q: %w", index+1, value, err)
			}
			if !inRequestWindow(start, request) {
				continue
			}
			slot := adapter.slot(venue, row, start, fetchedAt)
			slots[slot.ID] = slot
		}
	}
	venueList := make([]domain.Venue, 0, len(venues))
	for _, venue := range venues {
		venueList = append(venueList, venue)
	}
	sort.Slice(venueList, func(i, j int) bool { return venueList[i].ID < venueList[j].ID })
	slotList := make([]domain.AvailabilitySlot, 0, len(slots))
	for _, slot := range slots {
		slotList = append(slotList, slot)
	}
	sort.Slice(slotList, func(i, j int) bool { return slotList[i].ID < slotList[j].ID })
	return source.AvailabilitySnapshot{Venues: venueList, Slots: slotList}, nil
}

func validateRow(row browser.ActiveSGAvailability) error {
	if strings.TrimSpace(row.VenueID) == "" || strings.TrimSpace(row.VenueName) == "" || strings.TrimSpace(row.VenueURL) == "" || row.Date.IsZero() {
		return errors.New("venue ID, venue name, venue URL, and date are required")
	}
	switch row.AvailabilityType {
	case browser.ActiveSGInstant, browser.ActiveSGBallot, "":
	default:
		return fmt.Errorf("unknown availability type %q", row.AvailabilityType)
	}
	switch row.AvailabilityStatus {
	case browser.ActiveSGSlotsVisible, browser.ActiveSGBallotAvailable, browser.ActiveSGAlreadyBalloted, browser.ActiveSGNoHourlySlots:
	default:
		return fmt.Errorf("unknown availability status %q", row.AvailabilityStatus)
	}
	if row.AvailabilityStatus == browser.ActiveSGSlotsVisible && len(row.SlotStartTimes) == 0 {
		return errors.New("visible slots need slot start times")
	}
	return nil
}

func (adapter *Adapter) venue(row browser.ActiveSGAvailability, fetchedAt time.Time) domain.Venue {
	return domain.Venue{
		ID: adapter.info.ID + ":venue:" + row.VenueID, SourceIDs: []string{adapter.info.ID}, Name: row.VenueName,
		BookingURLs: []string{row.VenueURL}, Provenance: adapter.provenance(row.VenueID, fetchedAt),
	}
}

func (adapter *Adapter) slot(venue domain.Venue, row browser.ActiveSGAvailability, start, fetchedAt time.Time) domain.AvailabilitySlot {
	reference := strings.Join([]string{row.VenueID, start.UTC().Format(time.RFC3339), row.VenueURL}, "\x00")
	return domain.AvailabilitySlot{
		ID: adapter.info.ID + ":slot:" + stableID(reference), VenueID: venue.ID, SourceID: adapter.info.ID,
		Start: start.UTC(), End: start.Add(time.Hour).UTC(), Status: domain.AvailabilityAvailable, BookingURL: row.VenueURL,
		ObservedAt: fetchedAt, FetchedAt: fetchedAt, StaleAfter: adapter.staleAfter(fetchedAt), Provenance: adapter.provenance(reference, fetchedAt),
	}
}

func (adapter *Adapter) provenance(reference string, fetchedAt time.Time) domain.Provenance {
	return domain.Provenance{SourceID: adapter.info.ID, SourceReference: reference, ObservedAt: fetchedAt, FetchedAt: fetchedAt, AdapterVersion: adapterVersion, Confidence: 0.75}
}

func (adapter *Adapter) staleAfter(now time.Time) time.Time {
	minutes := adapter.refreshMinutes
	if minutes < 60 {
		minutes = 60
	}
	return now.Add(2 * time.Duration(minutes) * time.Minute)
}

func (adapter *Adapter) setSuccess(started time.Time, records int) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: "activesg_browser", LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: records}
}

func (adapter *Adapter) setFailure(started time.Time, err error, state domain.HealthState, category string) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: state, LastAttempt: &now, LastCategory: category, AccessFailures: []domain.AccessFailure{{Mode: AccessMode, Error: err.Error()}}, LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: adapter.health.ConsecutiveFailures + 1, LastError: err.Error()}
}

func activeSGStart(date time.Time, value string) (time.Time, error) {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	clock, err := time.Parse("15:04", value)
	if err != nil {
		return time.Time{}, errors.New("time must use HH:mm")
	}
	localDate := date.In(location)
	return time.Date(localDate.Year(), localDate.Month(), localDate.Day(), clock.Hour(), clock.Minute(), 0, 0, location), nil
}

func inRequestWindow(value time.Time, request source.AvailabilityRequest) bool {
	if !request.StartDate.IsZero() && value.Before(request.StartDate) {
		return false
	}
	return request.EndDate.IsZero() || value.Before(request.EndDate)
}

func stableID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}

func normalizedNames(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
