// Package onepa maps onePA's public, read-only availability response into
// Kaypoh's normalized badminton availability model.
package onepa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

const (
	SourceID            = "onepa"
	AccessMode          = "onepa"
	availabilityURL     = "https://www.onepa.gov.sg/facilities/availability"
	facilityMetadataURL = "https://www.onepa.gov.sg/-api/Facility/GetXMCFacility?facility=badmintoncourts"
	facilitySlotsURL    = "https://www.onepa.gov.sg/-api/Facility/GetFacilitySlots"
	adapterVersion      = "onepa-api-v3"
	defaultStaleAfter   = 30 * time.Minute
	onePARequestPace    = 2 * time.Second
)

// Adapter opens the public availability page once per refresh to establish an
// anonymous session, then posts the configured facility and date to the
// observed availability endpoint. It contains no booking operation.
type Adapter struct {
	info           domain.SourceInfo
	settings       config.SourceOnePA
	refreshMinutes int
	http           *source.HTTPClient

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(info domain.SourceInfo, settings config.Source, httpClient *source.HTTPClient) (*Adapter, error) {
	if info.ID != SourceID {
		return nil, fmt.Errorf("onePA adapter source ID mismatch %q", info.ID)
	}
	if httpClient == nil {
		return nil, errors.New("onePA HTTP client is required")
	}
	if err := validateSettings(settings.OnePA); err != nil {
		return nil, err
	}
	return &Adapter{info: info, settings: settings.OnePA, refreshMinutes: settings.RefreshMinutes, http: httpClient, health: domain.SourceHealth{SourceID: info.ID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

// DiscoverVenues is local-only because each successful availability response
// contains the venue and court names needed to persist its slots atomically.
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
	session, err := adapter.http.NewSession()
	if err != nil {
		adapter.setFailure(started, err)
		return source.AvailabilitySnapshot{}, err
	}
	if _, err := session.Fetch(ctx, source.HTTPRequest{SourceID: SourceID, URL: availabilityURL}); err != nil {
		err = fmt.Errorf("open onePA availability page: %w", err)
		adapter.setFailure(started, err)
		return source.AvailabilitySnapshot{}, err
	}
	metadata, err := session.Fetch(ctx, source.HTTPRequest{SourceID: SourceID, URL: facilityMetadataURL})
	if err != nil {
		err = fmt.Errorf("open onePA badminton facility metadata: %w", err)
		adapter.setFailure(started, err)
		return source.AvailabilitySnapshot{}, err
	}
	if err := onePAResponseContentError(metadata.Header.Get("Content-Type"), metadata.Body); err != nil {
		err = fmt.Errorf("open onePA badminton facility metadata: %w", err)
		adapter.setFailure(started, err)
		return source.AvailabilitySnapshot{}, err
	}

	venues := make(map[string]domain.Venue)
	slots := make(map[string]domain.AvailabilitySlot)
	requestsSent := 0
	for _, facilityID := range facilityIDs(adapter.settings) {
		for _, date := range requestedDates(request) {
			if requestsSent > 0 {
				if err := waitForOnePARequest(ctx); err != nil {
					adapter.setFailure(started, err)
					return source.AvailabilitySnapshot{}, err
				}
			}
			payload, err := json.Marshal(facilitySlotsRequest{SelectedFacility: facilityID, SelectedDate: date.Format("2006-01-02")})
			if err != nil {
				adapter.setFailure(started, err)
				return source.AvailabilitySnapshot{}, err
			}
			response, err := session.Fetch(ctx, source.HTTPRequest{SourceID: SourceID, Method: http.MethodPost, URL: facilitySlotsURL, Headers: onePAHeaders(), Body: payload})
			requestsSent++
			if err != nil {
				err = fmt.Errorf("fetch onePA facility %q on %s: %w", facilityID, date.Format("2006-01-02"), err)
				adapter.setFailure(started, err)
				return source.AvailabilitySnapshot{}, err
			}
			if err = onePAResponseContentError(response.Header.Get("Content-Type"), response.Body); err != nil {
				err = fmt.Errorf("fetch onePA facility %q on %s: %w", facilityID, date.Format("2006-01-02"), err)
				adapter.setFailure(started, err)
				return source.AvailabilitySnapshot{}, err
			}
			result, err := parseResponse(response.Body)
			if err != nil {
				err = fmt.Errorf("parse onePA facility %q on %s: %w", facilityID, date.Format("2006-01-02"), err)
				adapter.setFailure(started, err)
				return source.AvailabilitySnapshot{}, err
			}
			snapshot, err := adapter.normalize(facilityID, result, request, fetchedAt)
			if err != nil {
				err = fmt.Errorf("normalize onePA facility %q on %s: %w", facilityID, date.Format("2006-01-02"), err)
				adapter.setFailure(started, err)
				return source.AvailabilitySnapshot{}, err
			}
			for _, venue := range snapshot.Venues {
				venues[venue.ID] = venue
			}
			for _, slot := range snapshot.Slots {
				slots[slot.ID] = slot
			}
		}
	}
	result := source.AvailabilitySnapshot{Venues: sortedVenues(venues), Slots: sortedSlots(slots)}
	adapter.setSuccess(started, len(result.Slots))
	return result, nil
}

func (adapter *Adapter) Health(context.Context) (domain.SourceHealth, error) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	return adapter.health, nil
}

func validateSettings(settings config.SourceOnePA) error {
	if !settings.Enabled {
		return errors.New("onePA reader is not enabled")
	}
	if len(facilityIDs(settings)) == 0 {
		return errors.New("onePA reader needs facility_ids")
	}
	return nil
}

func facilityIDs(settings config.SourceOnePA) []string {
	unique := make(map[string]struct{}, len(settings.FacilityIDs))
	for _, id := range settings.FacilityIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			unique[id] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func requestedDates(request source.AvailabilityRequest) []time.Time {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	start := request.StartDate.In(location)
	if request.StartDate.IsZero() {
		start = time.Now().In(location)
	}
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	end := request.EndDate.In(location)
	if request.EndDate.IsZero() || !end.After(start) {
		end = start.AddDate(0, 0, 1)
	}
	result := []time.Time{}
	// The public endpoint returns slots for the one selected date. The website
	// groups three dates in its calendar UI, but its response is not a three-day
	// window, so querying every day is necessary to avoid silently omitting two
	// days out of three.
	for date := start; date.Before(end); date = date.AddDate(0, 0, 1) {
		result = append(result, date)
	}
	return result
}

func onePAHeaders() http.Header {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("Origin", "https://www.onepa.gov.sg")
	headers.Set("Referer", availabilityURL)
	return headers
}

func onePAResponseContentError(contentType string, body []byte) error {
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		return nil
	}
	if strings.Contains(strings.ToLower(string(body)), "incapsula") {
		return errors.New("availability endpoint was blocked by the provider's Incapsula web-application firewall")
	}
	return fmt.Errorf("expected JSON response, got %q", boundedMessage(contentType))
}

func waitForOnePARequest(ctx context.Context) error {
	timer := time.NewTimer(onePARequestPace)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type facilitySlotsRequest struct {
	SelectedFacility string `json:"selectedFacility"`
	SelectedDate     string `json:"selectedDate"`
}

type facilitySlotsResponse struct {
	ErrorMessages string                `json:"errorMessages"`
	HasError      bool                  `json:"hasError"`
	Response      *facilitySlotsPayload `json:"response"`
}

type facilitySlotsPayload struct {
	OutletName   string `json:"outletName"`
	ResourceList []struct {
		ResourceID   string `json:"resourceId"`
		ResourceName string `json:"resourceName"`
		SlotList     []struct {
			ActualStatus       string `json:"actualStatus"`
			AvailabilityStatus string `json:"availabilityStatus"`
			EndTime            string `json:"endTime"`
			IsAvailable        bool   `json:"isAvailable"`
			StartTime          string `json:"startTime"`
			TimeRangeID        string `json:"timeRangeId"`
		} `json:"slotList"`
	} `json:"resourceList"`
}

func parseResponse(body []byte) (facilitySlotsResponse, error) {
	var result facilitySlotsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return facilitySlotsResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if result.HasError {
		message := strings.TrimSpace(result.ErrorMessages)
		if message == "" {
			message = "onePA availability response reported an error"
		}
		return facilitySlotsResponse{}, errors.New(boundedMessage(message))
	}
	if result.Response == nil {
		return facilitySlotsResponse{}, errors.New("onePA availability response is missing response")
	}
	return result, nil
}

func (adapter *Adapter) normalize(facilityID string, response facilitySlotsResponse, request source.AvailabilityRequest, fetchedAt time.Time) (source.AvailabilitySnapshot, error) {
	venue := adapter.venue(facilityID, response.Response.OutletName, fetchedAt)
	slots := make(map[string]domain.AvailabilitySlot)
	for _, resource := range response.Response.ResourceList {
		for _, value := range resource.SlotList {
			if !value.IsAvailable {
				continue
			}
			start, err := onePATime(value.StartTime)
			if err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("resource %q slot start: %w", resource.ResourceName, err)
			}
			end, err := onePATime(value.EndTime)
			if err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("resource %q slot end: %w", resource.ResourceName, err)
			}
			if !end.After(start) {
				return source.AvailabilitySnapshot{}, fmt.Errorf("resource %q slot end must be after start", resource.ResourceName)
			}
			if !inRequestWindow(start, end, request) {
				continue
			}
			if strings.TrimSpace(resource.ResourceID) == "" || strings.TrimSpace(resource.ResourceName) == "" || strings.TrimSpace(value.TimeRangeID) == "" {
				return source.AvailabilitySnapshot{}, errors.New("available onePA slot needs resource ID, resource name, and time range ID")
			}
			slot := adapter.slot(venue, facilityID, resource.ResourceID, resource.ResourceName, value.TimeRangeID, start, end, fetchedAt)
			slots[slot.ID] = slot
		}
	}
	return source.AvailabilitySnapshot{Venues: []domain.Venue{venue}, Slots: sortedSlots(slots)}, nil
}

func onePATime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("must use an ISO date-time")
}

func (adapter *Adapter) venue(facilityID, outletName string, fetchedAt time.Time) domain.Venue {
	name := strings.TrimSpace(outletName)
	if name == "" {
		name = facilityID
	}
	reference := "venue\x00" + facilityID
	return domain.Venue{ID: SourceID + ":venue:" + stableID(facilityID), SourceIDs: []string{facilityID}, Name: name, BookingURLs: []string{availabilityURL + "?facilityId=" + facilityID}, Provenance: adapter.provenance(reference, fetchedAt)}
}

func (adapter *Adapter) slot(venue domain.Venue, facilityID, resourceID, resourceName, timeRangeID string, start, end, fetchedAt time.Time) domain.AvailabilitySlot {
	reference := strings.Join([]string{facilityID, resourceID, timeRangeID, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)}, "\x00")
	return domain.AvailabilitySlot{ID: SourceID + ":slot:" + stableID(reference), VenueID: venue.ID, CourtName: strings.TrimSpace(resourceName), SourceID: SourceID, Start: start.UTC(), End: end.UTC(), Status: domain.AvailabilityAvailable, BookingURL: availabilityURL + "?facilityId=" + facilityID, ObservedAt: fetchedAt, FetchedAt: fetchedAt, StaleAfter: adapter.staleAfter(fetchedAt), Provenance: adapter.provenance(reference, fetchedAt)}
}

func (adapter *Adapter) provenance(reference string, fetchedAt time.Time) domain.Provenance {
	return domain.Provenance{SourceID: SourceID, SourceReference: reference, ObservedAt: fetchedAt, FetchedAt: fetchedAt, AdapterVersion: adapterVersion, Confidence: 0.85}
}

func (adapter *Adapter) staleAfter(now time.Time) time.Time {
	interval := time.Duration(adapter.refreshMinutes) * time.Minute
	if interval < defaultStaleAfter {
		interval = defaultStaleAfter
	}
	return now.Add(2 * interval)
}

func (adapter *Adapter) setSuccess(started time.Time, records int) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: SourceID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: "onepa_api", LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: records}
}

func (adapter *Adapter) setFailure(started time.Time, err error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: SourceID, State: domain.HealthDegraded, LastAttempt: &now, LastCategory: "onepa_api_failed", AccessFailures: []domain.AccessFailure{{Mode: AccessMode, Error: err.Error()}}, LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: adapter.health.ConsecutiveFailures + 1, LastError: err.Error()}
}

func inRequestWindow(start, end time.Time, request source.AvailabilityRequest) bool {
	if !request.StartDate.IsZero() && !end.After(request.StartDate) {
		return false
	}
	return request.EndDate.IsZero() || start.Before(request.EndDate)
}

func sortedVenues(values map[string]domain.Venue) []domain.Venue {
	result := make([]domain.Venue, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func sortedSlots(values map[string]domain.AvailabilitySlot) []domain.AvailabilitySlot {
	result := make([]domain.AvailabilitySlot, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Start.Equal(result[j].Start) {
			return result[i].ID < result[j].ID
		}
		return result[i].Start.Before(result[j].Start)
	})
	return result
}

func stableID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}

func boundedMessage(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maximum = 320
	if len(value) > maximum {
		return value[:maximum] + "…"
	}
	return value
}
