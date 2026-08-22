// Package publicavailability reads the confirmed anonymous badminton
// availability surfaces. It does not contain any booking operation.
package publicavailability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

const adapterVersion = "public-availability-v1"

var supportedSourceIDs = map[string]struct{}{
	"sba-stadium":              {},
	"singapore-badminton-hall": {},
	"smash-arena":              {},
	"wyse-active":              {},
}

// Supports reports whether sourceID has a verified, anonymous reader.
func Supports(sourceID string) bool {
	_, ok := supportedSourceIDs[sourceID]
	return ok
}

// Adapter maps each provider's public availability payload to Kaypoh's
// badminton-only data model.
type Adapter struct {
	info     domain.SourceInfo
	settings config.Source
	http     *source.HTTPClient

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(info domain.SourceInfo, settings config.Source, httpClient *source.HTTPClient) (*Adapter, error) {
	if !Supports(info.ID) {
		return nil, fmt.Errorf("unsupported public availability source %q", info.ID)
	}
	if httpClient == nil {
		return nil, errors.New("HTTP client is required")
	}
	return &Adapter{info: info, settings: settings, http: httpClient, health: domain.SourceHealth{SourceID: info.ID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

// DiscoverVenues is intentionally local-only. A successful availability
// snapshot includes every venue it observed, which keeps venue and slot writes
// atomic in the application layer.
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
		snapshot source.AvailabilitySnapshot
		err      error
	)
	switch adapter.info.ID {
	case "sba-stadium":
		snapshot, err = adapter.fetchSBA(ctx, request)
	case "singapore-badminton-hall":
		snapshot, err = adapter.fetchPlaytomic(ctx, request)
	case "smash-arena":
		snapshot, err = adapter.fetchSmash(ctx, request)
	case "wyse-active":
		snapshot, err = adapter.fetchWyse(ctx, request)
	default:
		err = fmt.Errorf("unsupported public availability source %q", adapter.info.ID)
	}
	if err != nil {
		adapter.setFailure(started, err)
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

func (adapter *Adapter) setSuccess(started time.Time, records int) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: "public_availability", LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: records}
}

func (adapter *Adapter) setFailure(started time.Time, err error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	now := time.Now().UTC()
	adapter.health = domain.SourceHealth{SourceID: adapter.info.ID, State: domain.HealthDegraded, LastAttempt: &now, LastCategory: "public_availability", LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: adapter.health.ConsecutiveFailures + 1, LastError: err.Error()}
}

func (adapter *Adapter) staleAfter(fetchedAt time.Time) time.Time {
	interval := adapter.info.Policy.PollFloor
	if configured := time.Duration(adapter.settings.RefreshMinutes) * time.Minute; configured > interval {
		interval = configured
	}
	if interval < time.Minute {
		interval = 30 * time.Minute
	}
	return fetchedAt.Add(2 * interval)
}

func (adapter *Adapter) fetchSBA(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	const baseURL = "https://booking.singaporebadminton.org.sg"
	session, err := adapter.http.NewSession()
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	if _, err := session.Fetch(ctx, source.HTTPRequest{SourceID: adapter.info.ID, URL: baseURL + "/"}); err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("open public booking page: %w", err)
	}
	var location struct {
		Success  bool `json:"success"`
		Location struct {
			ID                int    `json:"id"`
			Name              string `json:"name"`
			PremiumCourtCount int    `json:"no_of_premium_court"`
		} `json:"location"`
	}
	if err := adapter.fetchJSON(ctx, session, source.HTTPRequest{SourceID: adapter.info.ID, URL: baseURL + "/api/location/default", Headers: refererHeaders(baseURL + "/")}, &location); err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public location: %w", err)
	}
	if !location.Success || location.Location.ID < 1 || location.Location.PremiumCourtCount < 1 {
		return source.AvailabilitySnapshot{}, errors.New("public SBA location payload has no premium courts")
	}
	csrf, found, err := session.CookieValue(baseURL+"/", "XSRF-TOKEN")
	if err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("read anti-forgery cookie: %w", err)
	}
	if !found {
		return source.AvailabilitySnapshot{}, errors.New("public SBA page did not issue an anti-forgery cookie")
	}
	csrf, err = url.QueryUnescape(csrf)
	if err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("decode anti-forgery cookie: %w", err)
	}
	fetchedAt := time.Now().UTC()
	venue := adapter.venue("guillemard", firstNonEmpty(location.Location.Name, "KFF Badminton Arena @ Guillemard"), "", baseURL+"/")
	var slots []domain.AvailabilitySlot
	for _, date := range singaporeDates(request) {
		for court := 1; court <= location.Location.PremiumCourtCount; court++ {
			payload, err := json.Marshal(struct {
				CourtType   string `json:"court_type"`
				CourtNumber string `json:"court_no"`
				BookingDate string `json:"booking_date"`
				LocationID  int    `json:"location_id"`
			}{CourtType: "premium", CourtNumber: strconv.Itoa(court), BookingDate: date.Format("2006-01-02"), LocationID: location.Location.ID})
			if err != nil {
				return source.AvailabilitySnapshot{}, err
			}
			headers := refererHeaders(baseURL + "/")
			headers.Set("Content-Type", "application/json")
			headers.Set("X-XSRF-TOKEN", csrf)
			var availability struct {
				Success          bool     `json:"success"`
				UnavailableSlots []string `json:"unavailable_slots"`
			}
			if err := adapter.fetchJSON(ctx, session, source.HTTPRequest{SourceID: adapter.info.ID, Method: http.MethodPost, URL: baseURL + "/spa/unavailable-slots", Headers: headers, Body: payload}, &availability); err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public SBA availability for %s court %d: %w", date.Format("2006-01-02"), court, err)
			}
			if !availability.Success {
				return source.AvailabilitySnapshot{}, fmt.Errorf("public SBA availability was rejected for %s court %d", date.Format("2006-01-02"), court)
			}
			unavailable := make(map[string]struct{}, len(availability.UnavailableSlots))
			for _, value := range availability.UnavailableSlots {
				unavailable[normalizeClock(value)] = struct{}{}
			}
			for _, hour := range sbaHours {
				start := dateAtHour(date, hour)
				if !withinRequest(start, start.Add(time.Hour), request) {
					continue
				}
				if _, blocked := unavailable[start.In(singapore()).Format("15:04:05")]; blocked {
					continue
				}
				reference := fmt.Sprintf("premium:%d:%s", court, start.In(singapore()).Format(time.RFC3339))
				slots = append(slots, adapter.slot(venue, reference, fmt.Sprintf("Premium Court %d", court), start, start.Add(time.Hour), nil, baseURL+"/", fetchedAt))
			}
		}
	}
	return snapshot(venue, slots), nil
}

var sbaHours = []int{7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25}

func (adapter *Adapter) fetchPlaytomic(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	type club struct {
		ID   string
		Slug string
		Name string
	}
	clubs := []club{
		{ID: "2b456235-408b-403d-9ff1-e8e5b861f339", Slug: "sbh-sims", Name: "SBH @ Sims"},
		{ID: "590a333e-b5ff-4854-9b20-865e8ab6ccf4", Slug: "sbh-east-coast-expo", Name: "SBH East Coast @ EXPO"},
		{ID: "ba8f5a0b-20c7-4cea-8a32-0694f9f74ae1", Slug: "sph", Name: "TSA @ EXPO"},
	}
	fetchedAt := time.Now().UTC()
	venues := make([]domain.Venue, 0, len(clubs))
	var slots []domain.AvailabilitySlot
	for _, club := range clubs {
		bookingURL := "https://playtomic.com/clubs/" + club.Slug
		venue := adapter.venue(club.Slug, club.Name, "", bookingURL)
		venues = append(venues, venue)
		resourceNames, err := adapter.playtomicResourceNames(ctx, club.Slug)
		if err != nil {
			return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public Playtomic club %s: %w", club.Slug, err)
		}
		for _, date := range singaporeDates(request) {
			endpoint := "https://playtomic.com/api/clubs/availability?" + url.Values{"tenant_id": {club.ID}, "date": {date.Format("2006-01-02")}, "sport_id": {"BADMINTON"}}.Encode()
			var availability []playtomicDay
			if err := adapter.fetchJSON(ctx, nil, source.HTTPRequest{SourceID: adapter.info.ID, URL: endpoint, Headers: refererHeaders(bookingURL), CacheTTL: 0}, &availability); err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public Playtomic availability for %s on %s: %w", club.Slug, date.Format("2006-01-02"), err)
			}
			for _, court := range availability {
				courtName := firstNonEmpty(resourceNames[court.ResourceID], court.ResourceID)
				for _, value := range court.Slots {
					start, err := localDateTime(firstNonEmpty(court.StartDate, date.Format("2006-01-02")), value.StartTime)
					if err != nil {
						return source.AvailabilitySnapshot{}, fmt.Errorf("parse public Playtomic slot: %w", err)
					}
					duration := time.Duration(value.Duration) * time.Minute
					if duration < time.Minute {
						return source.AvailabilitySnapshot{}, fmt.Errorf("public Playtomic slot has invalid duration %d", value.Duration)
					}
					end := start.Add(duration)
					if !withinRequest(start, end, request) {
						continue
					}
					price := parsePriceCents(value.Price)
					reference := strings.Join([]string{club.ID, court.ResourceID, start.Format(time.RFC3339), end.Format(time.RFC3339)}, ":")
					slots = append(slots, adapter.slot(venue, reference, courtName, start, end, price, bookingURL, fetchedAt))
				}
			}
		}
	}
	return source.AvailabilitySnapshot{Venues: venues, Slots: slots}, nil
}

type playtomicDay struct {
	ResourceID string          `json:"resource_id"`
	StartDate  string          `json:"start_date"`
	Slots      []playtomicSlot `json:"slots"`
}

type playtomicSlot struct {
	StartTime string `json:"start_time"`
	Duration  int    `json:"duration"`
	Price     string `json:"price"`
}

var playtomicResourcePattern = regexp.MustCompile(`(?s)"resourceId"\s*:\s*"([^"]+)".{0,800}?"name"\s*:\s*"([^"]+)".{0,800}?"sport"\s*:\s*"BADMINTON"`)

func (adapter *Adapter) playtomicResourceNames(ctx context.Context, slug string) (map[string]string, error) {
	response, err := adapter.http.Fetch(ctx, source.HTTPRequest{SourceID: adapter.info.ID, URL: "https://playtomic.com/clubs/" + slug, CacheTTL: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	body := strings.ReplaceAll(string(response.Body), `\"`, `"`)
	matches := playtomicResourcePattern.FindAllStringSubmatch(body, -1)
	resources := make(map[string]string, len(matches))
	for _, match := range matches {
		resources[match[1]] = html.UnescapeString(match[2])
	}
	return resources, nil
}

func (adapter *Adapter) fetchSmash(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	const baseURL = "https://booking.smasharena.sg"
	session, err := adapter.http.NewSession()
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	if _, err := session.Fetch(ctx, source.HTTPRequest{SourceID: adapter.info.ID, URL: baseURL + "/"}); err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("open public Smash booking page: %w", err)
	}
	fetchedAt := time.Now().UTC()
	venue := adapter.venue("smash-arena", "Smash Arena", "", baseURL+"/")
	var slots []domain.AvailabilitySlot
	for _, date := range singaporeDates(request) {
		dateText := date.Format("02-Jan-2006")
		var times smashEnvelope
		if err := adapter.fetchJSON(ctx, session, source.HTTPRequest{SourceID: adapter.info.ID, URL: baseURL + "/courts/getSmashSlot/" + dateText, Headers: refererHeaders(baseURL + "/")}, &times); err != nil {
			return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public Smash times for %s: %w", date.Format("2006-01-02"), err)
		}
		for _, timeID := range smashAvailableIDs(times.Slots, "avail_slot") {
			hour, err := smashHour(timeID)
			if err != nil {
				return source.AvailabilitySnapshot{}, err
			}
			start := dateAtHour(date, hour)
			if !withinRequest(start, start.Add(time.Hour), request) {
				continue
			}
			body := url.Values{"tim[]": {timeID}}.Encode()
			headers := refererHeaders(baseURL + "/")
			headers.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
			headers.Set("X-Requested-With", "XMLHttpRequest")
			var courts smashEnvelope
			if err := adapter.fetchJSON(ctx, session, source.HTTPRequest{SourceID: adapter.info.ID, Method: http.MethodPost, URL: baseURL + "/courts/getSmashCourt/" + dateText + "/0", Headers: headers, Body: []byte(body)}, &courts); err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public Smash courts for %s %s: %w", date.Format("2006-01-02"), timeID, err)
			}
			for _, court := range smashAvailableCourts(courts.Slots) {
				reference := strings.Join([]string{court.ID, start.Format(time.RFC3339)}, ":")
				slots = append(slots, adapter.slot(venue, reference, court.Name, start, start.Add(time.Hour), nil, baseURL+"/", fetchedAt))
			}
		}
	}
	return snapshot(venue, slots), nil
}

type smashEnvelope struct {
	Slots string `json:"slots"`
}

type smashCourt struct {
	ID   string
	Name string
}

var (
	spanPattern = regexp.MustCompile(`(?is)<span\b([^>]*)>(.*?)</span>`)
	attrPattern = regexp.MustCompile(`(?is)([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	tagPattern  = regexp.MustCompile(`(?is)<[^>]+>`)
)

func smashAvailableIDs(fragment, requiredClass string) []string {
	var result []string
	for _, span := range spanPattern.FindAllStringSubmatch(fragment, -1) {
		attributes := htmlAttributes(span[1])
		if hasClass(attributes["class"], requiredClass) && strings.TrimSpace(attributes["data-id"]) != "" {
			result = append(result, strings.TrimSpace(attributes["data-id"]))
		}
	}
	return result
}

func smashAvailableCourts(fragment string) []smashCourt {
	var result []smashCourt
	for _, span := range spanPattern.FindAllStringSubmatch(fragment, -1) {
		attributes := htmlAttributes(span[1])
		if !hasClass(attributes["class"], "available2") || strings.TrimSpace(attributes["data-id"]) == "" {
			continue
		}
		name := strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(span[2], "")))
		result = append(result, smashCourt{ID: strings.TrimSpace(attributes["data-id"]), Name: firstNonEmpty(name, attributes["data-id"])})
	}
	return result
}

func htmlAttributes(value string) map[string]string {
	attributes := make(map[string]string)
	for _, match := range attrPattern.FindAllStringSubmatch(value, -1) {
		attributes[strings.ToLower(match[1])] = firstNonEmpty(match[2], match[3], match[4])
	}
	return attributes
}

func hasClass(value, wanted string) bool {
	for _, class := range strings.Fields(value) {
		if class == wanted {
			return true
		}
	}
	return false
}

func smashHour(timeID string) (int, error) {
	value := strings.TrimPrefix(strings.TrimSpace(timeID), "time_")
	hour, err := strconv.Atoi(value)
	if err != nil || hour < 0 || hour > 25 {
		return 0, fmt.Errorf("invalid public Smash time identifier %q", timeID)
	}
	return hour, nil
}

func (adapter *Adapter) fetchWyse(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
	const (
		baseURL    = "https://wyseactivehub.rezerv.co"
		apiURL     = "https://customer-api.rezerv.co"
		locationID = "bdfe0e1f-b01a-48de-977c-2afbd2b15252"
	)
	type appointment struct {
		ID   string
		Name string
	}
	appointments := []appointment{
		{ID: "c3761aef-bc0c-477e-a7f8-f14250592fb5", Name: "Premier Court"},
		{ID: "96296a8a-5b90-4823-b9a1-e6d1fa1c6589", Name: "Ace Court"},
	}
	session, err := adapter.http.NewSession()
	if err != nil {
		return source.AvailabilitySnapshot{}, err
	}
	headers := refererHeaders(baseURL + "/")
	headers.Set("Origin", baseURL)
	if _, err := session.Fetch(ctx, source.HTTPRequest{SourceID: adapter.info.ID, URL: apiURL + "/v1/onboarding/get-session", Headers: headers}); err != nil {
		return source.AvailabilitySnapshot{}, fmt.Errorf("open public Wyse session: %w", err)
	}
	fetchedAt := time.Now().UTC()
	var venue domain.Venue
	var slots []domain.AvailabilitySlot
	for _, appointment := range appointments {
		detailsURL := apiURL + "/v1/appt-schedule/details?" + url.Values{"ApptId": {appointment.ID}, "LocationId": {locationID}}.Encode()
		var details wyseDetailsResponse
		if err := adapter.fetchJSON(ctx, session, source.HTTPRequest{SourceID: adapter.info.ID, URL: detailsURL, Headers: headers}, &details); err != nil {
			return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public Wyse %s details: %w", appointment.Name, err)
		}
		if details.Code != 0 {
			return source.AvailabilitySnapshot{}, fmt.Errorf("public Wyse %s details returned code %d", appointment.Name, details.Code)
		}
		if venue.ID == "" {
			venue = adapter.venue("wyse-active-hub", firstNonEmpty(details.Data.LocationName, "WYSE ACTIVE HUB"), details.Data.Address.Addr, baseURL+"/timetable")
			if latitude, longitude, ok := wyseCoordinates(details.Data.Address.Latitude, details.Data.Address.Longitude); ok {
				venue.Coordinates = domain.Coordinates{Latitude: latitude, Longitude: longitude}
			}
		}
		availableDates := make(map[string]struct{}, len(details.Data.AvailableSlotDates))
		for _, date := range details.Data.AvailableSlotDates {
			availableDates[date] = struct{}{}
		}
		for _, date := range singaporeDates(request) {
			if _, ok := availableDates[date.Format("2006-01-02")]; !ok {
				continue
			}
			utcDate := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, time.UTC)
			endpoint := apiURL + "/v3/appt-schedule/timeslot_calendar?" + url.Values{"apptId": {appointment.ID}, "locationId": {locationID}, "apptDate": {utcDate.Format(time.RFC3339Nano)}}.Encode()
			var calendar wyseCalendarResponse
			if err := adapter.fetchJSON(ctx, session, source.HTTPRequest{SourceID: adapter.info.ID, URL: endpoint, Headers: headers}, &calendar); err != nil {
				return source.AvailabilitySnapshot{}, fmt.Errorf("fetch public Wyse %s availability for %s: %w", appointment.Name, date.Format("2006-01-02"), err)
			}
			if calendar.Code != 0 {
				return source.AvailabilitySnapshot{}, fmt.Errorf("public Wyse %s availability returned code %d", appointment.Name, calendar.Code)
			}
			for _, court := range calendar.Data.ResourceSlots {
				for _, value := range court.Slots {
					if !strings.EqualFold(strings.TrimSpace(value.Status), "available") {
						continue
					}
					start, err := localDateTime(calendar.Data.AppointmentDate, value.StartTime)
					if err != nil {
						return source.AvailabilitySnapshot{}, fmt.Errorf("parse public Wyse slot: %w", err)
					}
					end, err := localDateTime(calendar.Data.AppointmentDate, value.EndTime)
					if err != nil {
						return source.AvailabilitySnapshot{}, fmt.Errorf("parse public Wyse slot: %w", err)
					}
					if !end.After(start) || !withinRequest(start, end, request) {
						continue
					}
					price := cents(value.Price)
					reference := strings.Join([]string{appointment.ID, court.ID, start.Format(time.RFC3339), end.Format(time.RFC3339)}, ":")
					bookingURL := baseURL + "/appointment-booking?" + url.Values{"apptId": {appointment.ID}, "locationId": {locationID}}.Encode()
					slots = append(slots, adapter.slot(venue, reference, firstNonEmpty(court.Name, appointment.Name), start, end, price, bookingURL, fetchedAt))
				}
			}
		}
	}
	if venue.ID == "" {
		return source.AvailabilitySnapshot{}, errors.New("public Wyse details did not identify a venue")
	}
	return snapshot(venue, slots), nil
}

type wyseDetailsResponse struct {
	Code int `json:"code"`
	Data struct {
		LocationName       string   `json:"locationName"`
		AvailableSlotDates []string `json:"availableSlotDates"`
		Address            struct {
			Addr      string `json:"addr"`
			Latitude  string `json:"lat"`
			Longitude string `json:"long"`
		} `json:"address"`
	} `json:"data"`
}

type wyseCalendarResponse struct {
	Code int `json:"code"`
	Data struct {
		AppointmentDate string `json:"appointmentDate"`
		ResourceSlots   []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Slots []struct {
				StartTime string  `json:"startTime"`
				EndTime   string  `json:"endTime"`
				Price     float64 `json:"price"`
				Status    string  `json:"status"`
			} `json:"slots"`
		} `json:"resourceSlots"`
	} `json:"data"`
}

func (adapter *Adapter) fetchJSON(ctx context.Context, session *source.HTTPSession, request source.HTTPRequest, target any) error {
	var (
		response source.HTTPResponse
		err      error
	)
	if session != nil {
		response, err = session.Fetch(ctx, request)
	} else {
		response, err = adapter.http.Fetch(ctx, request)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func (adapter *Adapter) venue(externalID, name, address, bookingURL string) domain.Venue {
	now := time.Now().UTC()
	return domain.Venue{ID: adapter.info.ID + ":venue:" + externalID, SourceIDs: []string{externalID}, Name: name, Address: address, BookingURLs: []string{bookingURL}, Provenance: adapter.provenance(externalID, now)}
}

func (adapter *Adapter) slot(venue domain.Venue, reference, courtName string, start, end time.Time, price *int64, bookingURL string, fetchedAt time.Time) domain.AvailabilitySlot {
	return domain.AvailabilitySlot{ID: adapter.info.ID + ":slot:" + stableID(reference), VenueID: venue.ID, CourtName: courtName, SourceID: adapter.info.ID, Start: start.UTC(), End: end.UTC(), Status: domain.AvailabilityAvailable, PriceCents: price, Currency: "SGD", BookingURL: bookingURL, ObservedAt: fetchedAt, FetchedAt: fetchedAt, StaleAfter: adapter.staleAfter(fetchedAt), Provenance: adapter.provenance(reference, fetchedAt)}
}

func (adapter *Adapter) provenance(reference string, observedAt time.Time) domain.Provenance {
	return domain.Provenance{SourceID: adapter.info.ID, SourceReference: reference, ObservedAt: observedAt, FetchedAt: observedAt, AdapterVersion: adapterVersion, Confidence: 1}
}

func snapshot(venue domain.Venue, slots []domain.AvailabilitySlot) source.AvailabilitySnapshot {
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Start.Equal(slots[j].Start) {
			return slots[i].CourtName < slots[j].CourtName
		}
		return slots[i].Start.Before(slots[j].Start)
	})
	return source.AvailabilitySnapshot{Venues: []domain.Venue{venue}, Slots: slots}
}

func singapore() *time.Location {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return location
}

func singaporeDates(request source.AvailabilityRequest) []time.Time {
	location := singapore()
	start := request.StartDate.In(location)
	end := request.EndDate.In(location)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, location)
	var dates []time.Time
	for date := start; date.Before(end); date = date.AddDate(0, 0, 1) {
		dates = append(dates, date)
	}
	return dates
}

func dateAtHour(date time.Time, hour int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, singapore()).UTC()
}

func localDateTime(date, clock string) (time.Time, error) {
	clock = strings.TrimSpace(clock)
	dayOffset := 0
	if days, remainder, found := strings.Cut(clock, "."); found {
		parsedDays, err := strconv.Atoi(days)
		if err != nil || parsedDays < 0 {
			return time.Time{}, fmt.Errorf("invalid day-prefixed clock %q", clock)
		}
		dayOffset = parsedDays
		clock = remainder
	}
	clock = normalizeClock(clock)
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(date)+" "+clock, singapore())
	if err != nil {
		return time.Time{}, err
	}
	return parsed.AddDate(0, 0, dayOffset).UTC(), nil
}

func normalizeClock(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == len("15:04") {
		return value + ":00"
	}
	return value
}

func withinRequest(start, end time.Time, request source.AvailabilityRequest) bool {
	return start.Before(request.EndDate) && end.After(request.StartDate)
}

func refererHeaders(referer string) http.Header {
	headers := make(http.Header)
	headers.Set("Referer", referer)
	headers.Set("Accept", "application/json, text/plain, */*")
	return headers
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stableID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}

func parsePriceCents(value string) *int64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil
	}
	amount, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || amount < 0 {
		return nil
	}
	return cents(amount)
}

func cents(value float64) *int64 {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	amount := int64(math.Round(value * 100))
	return &amount
}

func wyseCoordinates(latitude, longitude string) (float64, float64, bool) {
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(latitude), 64)
	lng, lngErr := strconv.ParseFloat(strings.TrimSpace(longitude), 64)
	coordinates := domain.Coordinates{Latitude: lat, Longitude: lng}
	return lat, lng, latErr == nil && lngErr == nil && coordinates.Valid()
}
