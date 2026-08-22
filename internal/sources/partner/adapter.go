// Package partner adapts approved partner availability integrations into the
// local badminton-only model. It deliberately exposes no booking operation.
package partner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

type Spec struct {
	ID string
}

type Adapter struct {
	info     domain.SourceInfo
	spec     Spec
	settings config.Source
	http     *source.HTTPClient
	browser  browser.Fetcher

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(info domain.SourceInfo, spec Spec, settings config.Source, httpClient *source.HTTPClient, browserClient browser.Fetcher) (*Adapter, error) {
	if info.ID == "" || spec.ID != info.ID {
		return nil, fmt.Errorf("partner adapter source ID mismatch %q", info.ID)
	}
	if httpClient == nil || browserClient == nil {
		return nil, errors.New("HTTP and browser clients are required")
	}
	return &Adapter{info: info, spec: spec, settings: settings, http: httpClient, browser: browserClient, health: domain.SourceHealth{SourceID: info.ID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

// DiscoverVenues is intentionally local-only. Live availability snapshots carry
// their venue identity so they can be stored atomically with the slots.
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
	var failures []error
	if adapter.settings.API.Enabled {
		snapshot, err := adapter.fetchAPI(ctx, request)
		if err == nil {
			adapter.setSuccess(started, len(snapshot.Slots), "api")
			return snapshot, nil
		}
		failures = append(failures, fmt.Errorf("API: %w", err))
	}
	if adapter.settings.Browser.Enabled {
		snapshot, err := adapter.fetchBrowser(ctx, request, adapter.settings.Browser)
		if err == nil {
			adapter.setSuccess(started, len(snapshot.Slots), "browser")
			return snapshot, nil
		}
		failures = append(failures, fmt.Errorf("browser: %w", err))
	}
	if adapter.settings.Public.Enabled {
		snapshot, err := adapter.fetchPublic(ctx, request)
		if err == nil {
			adapter.setSuccess(started, len(snapshot.Slots), "public")
			return snapshot, nil
		}
		failures = append(failures, fmt.Errorf("public: %w", err))
	}
	if len(failures) == 0 {
		err := errors.New("no approved availability access mode is configured")
		adapter.setFailure(started, err, domain.HealthCredentials, "not_configured")
		return source.AvailabilitySnapshot{}, err
	}
	err := errors.Join(failures...)
	state := domain.HealthDegraded
	category := "all_access_modes_failed"
	if !adapter.settings.Public.Enabled && !adapter.settings.Browser.Enabled && adapter.settings.API.Enabled {
		state = domain.HealthCredentials
		category = "api_unavailable"
	}
	adapter.setFailure(started, err, state, category)
	return source.AvailabilitySnapshot{}, err
}

func (adapter *Adapter) Health(context.Context) (domain.SourceHealth, error) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	return adapter.health, nil
}

func (adapter *Adapter) fetchAPI(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	api := adapter.settings.API
	if strings.TrimSpace(api.BaseURL) == "" || strings.TrimSpace(api.AvailabilityPath) == "" {
		return source.AvailabilitySnapshot{}, errors.New("base_url and availability_path are required")
	}
	endpoint, err := endpointURL(api.BaseURL, api.AvailabilityPath, request)
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	headers := make(http.Header)
	if strings.TrimSpace(api.BearerToken) != "" {
		headers.Set("Authorization", "Bearer "+api.BearerToken)
	}
	response, err := adapter.http.Fetch(ctx, source.HTTPRequest{SourceID: adapter.info.ID, URL: endpoint, Headers: headers, CacheTTL: 0})
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	snapshot, err := decodeSnapshot(response.Body, adapter.info.ID, request, response.FetchedAt, adapter.staleAfter(response.FetchedAt))
	if err != nil || strings.TrimSpace(api.VenuesPath) == "" {
		return snapshot, err
	}
	venueEndpoint, err := endpointBaseURL(api.BaseURL, api.VenuesPath)
	if err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("build venues endpoint: %w", err)
	}
	venueResponse, err := adapter.http.Fetch(ctx, source.HTTPRequest{SourceID: adapter.info.ID, URL: venueEndpoint.String(), Headers: headers, CacheTTL: 5 * time.Minute})
	if err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("fetch venues: %w", err)
	}
	venues, err := decodeVenues(venueResponse.Body, adapter.info.ID, venueResponse.FetchedAt)
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	snapshot.Venues = mergeVenues(snapshot.Venues, venues)
	return snapshot, nil
}

func (adapter *Adapter) fetchBrowser(ctx context.Context, request source.AvailabilityRequest, settings config.SourceBrowser) (source.AvailabilitySnapshot, error) {
	if settings.AvailabilityURL == "" || settings.SlotJSONSelector == "" {
		return source.AvailabilitySnapshot{}, errors.New("availability_url and slot_json_selector are required")
	}
	availabilityURL := expandAvailabilityURL(settings.AvailabilityURL, request)
	body, err := adapter.browser.Fetch(ctx, browser.Request{
		URL: availabilityURL, LoginURL: settings.LoginURL, Username: settings.Username, Password: settings.Password,
		UsernameSelector: settings.UsernameSelector, PasswordSelector: settings.PasswordSelector, SubmitSelector: settings.SubmitSelector,
		ReadySelector: settings.ReadySelector, JSONSelector: settings.SlotJSONSelector, SessionStateBase64: settings.SessionStateBase64,
		PermittedHosts: adapter.info.Policy.PermittedHosts, Timeout: adapter.info.Policy.Timeout,
	})
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	return decodeSnapshot(body, adapter.info.ID, request, time.Now().UTC(), adapter.staleAfter(time.Now().UTC()))
}

func (adapter *Adapter) fetchPublic(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	settings := adapter.settings.Public
	if settings.AvailabilityURL == "" || settings.SlotJSONSelector == "" {
		return source.AvailabilitySnapshot{}, errors.New("availability_url and slot_json_selector are required")
	}
	body, err := adapter.browser.Fetch(ctx, browser.Request{URL: expandAvailabilityURL(settings.AvailabilityURL, request), JSONSelector: settings.SlotJSONSelector, PermittedHosts: adapter.info.Policy.PermittedHosts, Timeout: adapter.info.Policy.Timeout})
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	now := time.Now().UTC()
	return decodeSnapshot(body, adapter.info.ID, request, now, adapter.staleAfter(now))
}

// expandAvailabilityURL permits partner configs to include {start_date} and
// {end_date}; no other interpolation is performed.
func expandAvailabilityURL(raw string, request source.AvailabilityRequest) string {
	replacements := strings.NewReplacer(
		"{start_date}", request.StartDate.Format("2006-01-02"),
		"{end_date}", request.EndDate.Format("2006-01-02"),
	)
	return replacements.Replace(raw)
}

func (adapter *Adapter) staleAfter(now time.Time) time.Time {
	interval := adapter.settings.RefreshMinutes
	if interval < 1 {
		interval = 30
	}
	return now.Add(2 * time.Duration(interval) * time.Minute)
}

func (adapter *Adapter) setSuccess(started time.Time, records int, mode string) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: mode, LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: records}
}

func (adapter *Adapter) setFailure(started time.Time, err error, state domain.HealthState, category string) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: state, LastAttempt: &now, LastCategory: category, LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: adapter.health.ConsecutiveFailures + 1, LastError: err.Error()}
}

func endpointURL(baseURL, path string, request source.AvailabilityRequest) (string, error) {
	base, err := endpointBaseURL(baseURL, path)
	if err != nil {
		return "", err
	}
	query := base.Query()
	query.Set("start_date", request.StartDate.Format("2006-01-02"))
	query.Set("end_date", request.EndDate.Format("2006-01-02"))
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func endpointBaseURL(baseURL, path string) (*url.URL, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme != "https" || base.Hostname() == "" {
		return nil, errors.New("base_url must be an HTTPS URL")
	}
	parsed, err := url.Parse(path)
	if err != nil || parsed.IsAbs() {
		return nil, errors.New("path must be a relative URL path")
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/" + strings.TrimPrefix(parsed.Path, "/")
	query := base.Query()
	for key, values := range parsed.Query() {
		query[key] = values
	}
	base.RawQuery = query.Encode()
	return base, nil
}

type payload struct {
	Venues       []wireVenue `json:"venues"`
	Slots        []wireSlot  `json:"slots"`
	Availability []wireSlot  `json:"availability"`
}

type wireVenue struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Address     string   `json:"address"`
	PostalCode  string   `json:"postal_code"`
	Latitude    *float64 `json:"latitude"`
	Longitude   *float64 `json:"longitude"`
	Indoor      *bool    `json:"indoor"`
	Sheltered   *bool    `json:"sheltered"`
	BookingURL  string   `json:"booking_url"`
	BookingURLs []string `json:"booking_urls"`
}

type wireSlot struct {
	ID                 string `json:"id"`
	VenueID            string `json:"venue_id"`
	VenueName          string `json:"venue_name"`
	VenueAddress       string `json:"venue_address"`
	CourtID            string `json:"court_id"`
	CourtName          string `json:"court_name"`
	FacilityID         string `json:"facility_id"`
	FacilityName       string `json:"facility_name"`
	Start              string `json:"start"`
	StartAt            string `json:"start_at"`
	End                string `json:"end"`
	EndAt              string `json:"end_at"`
	Status             string `json:"status"`
	PriceCents         *int64 `json:"price_cents"`
	Currency           string `json:"currency"`
	BookingURL         string `json:"booking_url"`
	MembershipRequired *bool  `json:"membership_required"`
}

func decodeSnapshot(body []byte, sourceID string, request source.AvailabilityRequest, fetchedAt, staleAfter time.Time) (source.AvailabilitySnapshot, error) {
	var document payload
	if err := json.Unmarshal(body, &document); err != nil {
		var slots []wireSlot
		if listErr := json.Unmarshal(body, &slots); listErr != nil {
			return source.AvailabilitySnapshot{}, errors.New("availability payload must be JSON with slots or availability")
		}
		document.Slots = slots
	}
	if len(document.Slots) == 0 && len(document.Availability) > 0 {
		document.Slots = document.Availability
	}
	venues := make(map[string]domain.Venue, len(document.Venues)+len(document.Slots))
	for _, value := range document.Venues {
		venue, err := normalizeVenue(value, sourceID, fetchedAt)
		if err != nil {
			return source.AvailabilitySnapshot{}, err
		}
		venues[venue.ID] = venue
	}
	slots := make([]domain.AvailabilitySlot, 0, len(document.Slots))
	for index, value := range document.Slots {
		slot, venue, err := normalizeSlot(value, sourceID, fetchedAt, staleAfter)
		if err != nil {
			return source.AvailabilitySnapshot{}, fmt.Errorf("normalize slot %d: %w", index+1, err)
		}
		if !slot.Start.Before(request.EndDate) || !slot.End.After(request.StartDate) {
			continue
		}
		if existing, ok := venues[venue.ID]; ok {
			venue = mergeVenue(existing, venue)
		}
		venues[venue.ID] = venue
		slots = append(slots, slot)
	}
	result := make([]domain.Venue, 0, len(venues))
	for _, venue := range venues {
		result = append(result, venue)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return source.AvailabilitySnapshot{Venues: result, Slots: slots}, nil
}

func decodeVenues(body []byte, sourceID string, fetchedAt time.Time) ([]domain.Venue, error) {
	var document payload
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, errors.New("venues payload must be JSON with a venues array")
	}
	venues := make([]domain.Venue, 0, len(document.Venues))
	for index, value := range document.Venues {
		venue, err := normalizeVenue(value, sourceID, fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("normalize venue %d: %w", index+1, err)
		}
		venues = append(venues, venue)
	}
	return venues, nil
}

func mergeVenues(first, second []domain.Venue) []domain.Venue {
	values := make(map[string]domain.Venue, len(first)+len(second))
	for _, venue := range first {
		values[venue.ID] = venue
	}
	for _, venue := range second {
		if existing, ok := values[venue.ID]; ok {
			values[venue.ID] = mergeVenue(venue, existing)
			continue
		}
		values[venue.ID] = venue
	}
	result := make([]domain.Venue, 0, len(values))
	for _, venue := range values {
		result = append(result, venue)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func normalizeVenue(value wireVenue, sourceID string, fetchedAt time.Time) (domain.Venue, error) {
	externalID := strings.TrimSpace(value.ID)
	if externalID == "" {
		return domain.Venue{}, errors.New("venue id is required")
	}
	name := strings.TrimSpace(value.Name)
	if name == "" {
		return domain.Venue{}, fmt.Errorf("venue %q name is required", externalID)
	}
	bookingURLs := append([]string(nil), value.BookingURLs...)
	if value.BookingURL != "" {
		bookingURLs = append(bookingURLs, value.BookingURL)
	}
	venue := domain.Venue{ID: venueID(sourceID, externalID), SourceIDs: []string{externalID}, Name: name, Address: strings.TrimSpace(value.Address), PostalCode: strings.TrimSpace(value.PostalCode), Indoor: value.Indoor, Sheltered: value.Sheltered, BookingURLs: bookingURLs, Provenance: provenance(sourceID, externalID, fetchedAt)}
	if value.Latitude != nil && value.Longitude != nil {
		venue.Coordinates = domain.Coordinates{Latitude: *value.Latitude, Longitude: *value.Longitude}
		if !venue.Coordinates.Valid() {
			return domain.Venue{}, fmt.Errorf("venue %q has invalid coordinates", externalID)
		}
	}
	return venue, nil
}

func normalizeSlot(value wireSlot, sourceID string, fetchedAt, staleAfter time.Time) (domain.AvailabilitySlot, domain.Venue, error) {
	externalVenueID := strings.TrimSpace(value.VenueID)
	if externalVenueID == "" {
		return domain.AvailabilitySlot{}, domain.Venue{}, errors.New("slot venue_id is required")
	}
	start, err := parseTime(first(value.StartAt, value.Start))
	if err != nil {
		return domain.AvailabilitySlot{}, domain.Venue{}, fmt.Errorf("slot start: %w", err)
	}
	end, err := parseTime(first(value.EndAt, value.End))
	if err != nil {
		return domain.AvailabilitySlot{}, domain.Venue{}, fmt.Errorf("slot end: %w", err)
	}
	if !end.After(start) {
		return domain.AvailabilitySlot{}, domain.Venue{}, errors.New("slot end must be after start")
	}
	status := domain.AvailabilityStatus(strings.ToLower(strings.TrimSpace(value.Status)))
	if status == "" {
		status = domain.AvailabilityAvailable
	}
	if status != domain.AvailabilityAvailable && status != domain.AvailabilityUnavailable && status != domain.AvailabilityUnknown {
		return domain.AvailabilitySlot{}, domain.Venue{}, fmt.Errorf("unknown slot status %q", value.Status)
	}
	courtName := first(value.CourtName, value.FacilityName)
	externalSlotID := strings.TrimSpace(value.ID)
	if externalSlotID == "" {
		externalSlotID = stableID(externalVenueID, first(value.CourtID, value.FacilityID, courtName), start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	}
	venueName := strings.TrimSpace(value.VenueName)
	if venueName == "" {
		venueName = externalVenueID
	}
	venue := domain.Venue{ID: venueID(sourceID, externalVenueID), SourceIDs: []string{externalVenueID}, Name: venueName, Address: strings.TrimSpace(value.VenueAddress), BookingURLs: urls(value.BookingURL), Provenance: provenance(sourceID, externalVenueID, fetchedAt)}
	slot := domain.AvailabilitySlot{ID: sourceID + ":slot:" + externalSlotID, VenueID: venue.ID, CourtName: strings.TrimSpace(courtName), SourceID: sourceID, Start: start, End: end, Status: status, PriceCents: value.PriceCents, Currency: first(value.Currency, "SGD"), MembershipRequired: value.MembershipRequired, BookingURL: strings.TrimSpace(value.BookingURL), ObservedAt: fetchedAt, FetchedAt: fetchedAt, StaleAfter: staleAfter, Provenance: provenance(sourceID, externalSlotID, fetchedAt)}
	return slot, venue, nil
}

func parseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("is required")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	location, _ := time.LoadLocation(domain.SingaporeTimeZone)
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be RFC3339 or YYYY-MM-DD HH:MM: %w", err)
	}
	return parsed.UTC(), nil
}

func mergeVenue(first, second domain.Venue) domain.Venue {
	if first.Name == "" {
		first.Name = second.Name
	}
	if first.Address == "" {
		first.Address = second.Address
	}
	if len(first.BookingURLs) == 0 {
		first.BookingURLs = second.BookingURLs
	}
	return first
}

func venueID(sourceID, externalID string) string { return sourceID + ":venue:" + externalID }

func provenance(sourceID, reference string, now time.Time) domain.Provenance {
	return domain.Provenance{SourceID: sourceID, SourceReference: reference, ObservedAt: now, FetchedAt: now, AdapterVersion: "partner-v1", Confidence: 1}
}

func urls(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{strings.TrimSpace(value)}
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stableID(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:12])
}
