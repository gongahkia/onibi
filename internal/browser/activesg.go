package browser

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	ActiveSGInstant         = "instant"
	ActiveSGBallot          = "ballot"
	ActiveSGSlotsVisible    = "slots_visible"
	ActiveSGBallotAvailable = "ballot_available"
	ActiveSGAlreadyBalloted = "already_balloted"
	ActiveSGNoHourlySlots   = "no_hourly_slots_visible"

	activeSGVenueProcedure    = "venue.listByActivity"
	activeSGScheduleProcedure = "schedule.listAvailable"
)

//go:embed activesg_transport.js
var activeSGTransportScript string

// ActiveSGScanRequest scopes a read-only API scan to one badminton activity.
// The imported Playwright storage state is used in memory only.
type ActiveSGScanRequest struct {
	VenueListURL       string
	VenueNames         []string
	ScanAll            bool
	SessionStateBase64 string
	PermittedHosts     []string
	Timeout            time.Duration
}

// ActiveSGSlot is one aggregate time returned by schedule.listAvailable. The
// subvenue IDs identify the facilities available at that time, but the endpoint
// does not include stable human-readable court names.
type ActiveSGSlot struct {
	Start       time.Time
	End         time.Time
	SubvenueIDs []string
}

// ActiveSGAvailability is one typed schedule result for a venue and date.
type ActiveSGAvailability struct {
	VenueID            string
	VenueName          string
	VenueURL           string
	VenueAddress       string
	VenuePostalCode    string
	VenueLatitude      float64
	VenueLongitude     float64
	Date               time.Time
	DateLabel          string
	AvailabilityType   string
	AvailabilityStatus string
	Slots              []ActiveSGSlot
}

// ActiveSGScanner keeps the provider adapter testable without starting the
// Playwright request driver.
type ActiveSGScanner interface {
	ScanActiveSG(context.Context, ActiveSGScanRequest) ([]ActiveSGAvailability, error)
}

type activeSGVenue struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	Status     string  `json:"status"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	PostalCode string  `json:"postalCode"`
}

type activeSGScheduleDay struct {
	Type      string                 `json:"type"`
	Timeslots []activeSGScheduleSlot `json:"timeslots"`
}

type activeSGScheduleSlot struct {
	Start     int64 `json:"start"`
	End       int64 `json:"end"`
	Subvenues []struct {
		ID string `json:"id"`
	} `json:"subvenues"`
}

type activeSGEnvelope[T any] struct {
	Result struct {
		Data struct {
			JSON T `json:"json"`
		} `json:"data"`
	} `json:"result"`
	Error json.RawMessage `json:"error"`
}

// ScanActiveSG calls the same read-only tRPC endpoints used by the venue and
// schedule pages. It does not render the page or touch booking/ballot controls.
func (client *Playwright) ScanActiveSG(ctx context.Context, request ActiveSGScanRequest) ([]ActiveSGAvailability, error) {
	if err := validateActiveSGRequest(request); err != nil {
		return nil, err
	}
	if err := acquire(ctx, client.sem); err != nil {
		return nil, err
	}
	defer func() { <-client.sem }()
	state, err := activeSGStorageState(request.SessionStateBase64)
	if err != nil {
		return nil, err
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	activityID, err := activeSGActivityID(request.VenueListURL)
	if err != nil {
		return nil, err
	}
	venueEndpoint, err := activeSGVenueEndpoint(request, activityID)
	if err != nil {
		return nil, err
	}
	venueBodies, err := activeSGNodeRequests(ctx, state, []string{venueEndpoint}, timeout)
	if err != nil {
		return nil, fmt.Errorf("read ActiveSG venue list: %w", err)
	}
	venues, err := activeSGVenueList(venueBodies[0], request)
	if err != nil {
		return nil, err
	}
	scheduleEndpoints := make([]string, 0, len(venues))
	for _, venue := range venues {
		endpoint, err := activeSGScheduleEndpoint(request, activityID, venue.ID)
		if err != nil {
			return nil, err
		}
		scheduleEndpoints = append(scheduleEndpoints, endpoint)
	}
	scheduleBodies, err := activeSGNodeRequests(ctx, state, scheduleEndpoints, timeout)
	if err != nil {
		return nil, fmt.Errorf("read ActiveSG schedules: %w", err)
	}
	results := make([]ActiveSGAvailability, 0, len(venues)*4)
	for index, venue := range venues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := activeSGVenueSchedule(scheduleBodies[index], request, activityID, venue)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
	}
	return results, nil
}

func activeSGVenueEndpoint(request ActiveSGScanRequest, activityID string) (string, error) {
	input := map[string]any{
		"json": map[string]any{"activityId": activityID, "postalCode": nil},
		"meta": map[string]any{"values": map[string]any{"postalCode": []string{"undefined"}}},
	}
	return activeSGProcedureURL(request.VenueListURL, activeSGVenueProcedure, input, request.PermittedHosts)
}

func activeSGVenueList(body json.RawMessage, request ActiveSGScanRequest) ([]activeSGVenue, error) {
	var envelope activeSGEnvelope[[]activeSGVenue]
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode ActiveSG venue list: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, errors.New("ActiveSG venue list returned a tRPC error")
	}
	venues := make([]activeSGVenue, 0, len(envelope.Result.Data.JSON))
	for _, venue := range envelope.Result.Data.JSON {
		if strings.TrimSpace(venue.ID) == "" || strings.TrimSpace(venue.Name) == "" {
			return nil, errors.New("ActiveSG venue list contains a venue without an ID or name")
		}
		if venue.Status != "" && venue.Status != "ACTIVE" {
			continue
		}
		if matchesActiveSGVenue(venue.Name, request.VenueNames, request.ScanAll) {
			venues = append(venues, venue)
		}
	}
	if len(venues) == 0 {
		return nil, errors.New("ActiveSG venue list did not match any requested venue")
	}
	sort.Slice(venues, func(i, j int) bool { return venues[i].ID < venues[j].ID })
	return venues, nil
}

func activeSGScheduleEndpoint(request ActiveSGScanRequest, activityID, venueID string) (string, error) {
	input := map[string]any{"json": map[string]any{"venueId": venueID, "activityId": activityID}}
	return activeSGProcedureURL(request.VenueListURL, activeSGScheduleProcedure, input, request.PermittedHosts)
}

func activeSGVenueSchedule(body json.RawMessage, request ActiveSGScanRequest, activityID string, venue activeSGVenue) ([]ActiveSGAvailability, error) {
	var envelope activeSGEnvelope[[]json.RawMessage]
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode ActiveSG schedule at %q: %w", venue.Name, err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, fmt.Errorf("ActiveSG schedule at %q returned a tRPC error", venue.Name)
	}
	return decodeActiveSGSchedule(envelope.Result.Data.JSON, request.VenueListURL, activityID, venue)
}

func decodeActiveSGSchedule(rawRows []json.RawMessage, venueListURL, activityID string, venue activeSGVenue) ([]ActiveSGAvailability, error) {
	location, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	rows := make([]ActiveSGAvailability, 0, len(rawRows))
	for index, raw := range rawRows {
		var tuple []json.RawMessage
		if err := json.Unmarshal(raw, &tuple); err != nil || len(tuple) != 2 {
			return nil, fmt.Errorf("decode schedule row %d: expected [date, availability]", index+1)
		}
		var dateValue string
		var day activeSGScheduleDay
		if err := json.Unmarshal(tuple[0], &dateValue); err != nil {
			return nil, fmt.Errorf("decode schedule row %d date: %w", index+1, err)
		}
		date, err := time.ParseInLocation("2006-01-02", dateValue, location)
		if err != nil {
			return nil, fmt.Errorf("decode schedule row %d date: %w", index+1, err)
		}
		if err := json.Unmarshal(tuple[1], &day); err != nil {
			return nil, fmt.Errorf("decode schedule row %d availability: %w", index+1, err)
		}
		if day.Type != ActiveSGInstant && day.Type != ActiveSGBallot {
			return nil, fmt.Errorf("decode schedule row %d: unknown availability type %q", index+1, day.Type)
		}
		slots := make([]ActiveSGSlot, 0, len(day.Timeslots))
		for slotIndex, rawSlot := range day.Timeslots {
			start := time.UnixMilli(rawSlot.Start)
			end := time.UnixMilli(rawSlot.End)
			if rawSlot.Start <= 0 || !end.After(start) || start.In(location).Format("2006-01-02") != dateValue {
				return nil, fmt.Errorf("decode schedule row %d slot %d: invalid time range", index+1, slotIndex+1)
			}
			subvenueIDs := make([]string, 0, len(rawSlot.Subvenues))
			seen := make(map[string]struct{}, len(rawSlot.Subvenues))
			for _, subvenue := range rawSlot.Subvenues {
				id := strings.TrimSpace(subvenue.ID)
				if id == "" {
					return nil, fmt.Errorf("decode schedule row %d slot %d: empty subvenue ID", index+1, slotIndex+1)
				}
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					subvenueIDs = append(subvenueIDs, id)
				}
			}
			sort.Strings(subvenueIDs)
			slots = append(slots, ActiveSGSlot{Start: start, End: end, SubvenueIDs: subvenueIDs})
		}
		status := ActiveSGNoHourlySlots
		if day.Type == ActiveSGBallot {
			status = ActiveSGBallotAvailable
		} else if len(slots) > 0 {
			status = ActiveSGSlotsVisible
		}
		venueURL, err := activeSGTimeslotURL(venueListURL, activityID, venue.ID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, ActiveSGAvailability{
			VenueID: venue.ID, VenueName: venue.Name, VenueURL: venueURL,
			VenueAddress: venue.Address, VenuePostalCode: venue.PostalCode,
			VenueLatitude: venue.Latitude, VenueLongitude: venue.Longitude,
			Date: date, DateLabel: date.Format("Mon, 2 Jan"), AvailabilityType: day.Type,
			AvailabilityStatus: status, Slots: slots,
		})
	}
	return rows, nil
}

func activeSGStorageState(encoded string) (json.RawMessage, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("decode imported browser session: invalid base64")
	}
	var state struct {
		Cookies []json.RawMessage `json:"cookies"`
		Origins []json.RawMessage `json:"origins"`
	}
	if err := json.Unmarshal(decoded, &state); err != nil {
		return nil, errors.New("decode imported browser session: invalid storage-state JSON")
	}
	if state.Cookies == nil || state.Origins == nil {
		return nil, errors.New("decode imported browser session: missing cookies storage-state fields")
	}
	return json.RawMessage(decoded), nil
}

type activeSGNodeRequest struct {
	StorageState        json.RawMessage `json:"storageState"`
	URLs                []string        `json:"urls"`
	TimeoutMilliseconds float64         `json:"timeoutMilliseconds"`
}

type activeSGNodeResponse struct {
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Body       string `json:"body"`
}

func activeSGNodeRequests(ctx context.Context, state json.RawMessage, endpoints []string, timeout time.Duration) ([]json.RawMessage, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, errors.New("ActiveSG API reader needs Node.js 20 or newer in PATH")
	}
	request := activeSGNodeRequest{StorageState: state, URLs: endpoints, TimeoutMilliseconds: float64(timeout.Milliseconds())}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode ActiveSG transport request: %w", err)
	}
	command := exec.CommandContext(ctx, node, "-e", activeSGTransportScript)
	command.Stdin = strings.NewReader(string(input))
	output, err := command.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			detail := strings.TrimSpace(string(exitError.Stderr))
			if detail != "" {
				return nil, fmt.Errorf("ActiveSG API transport: %s", detail)
			}
		}
		return nil, fmt.Errorf("ActiveSG API transport: %w", err)
	}
	var responses []activeSGNodeResponse
	if err := json.Unmarshal(output, &responses); err != nil {
		return nil, fmt.Errorf("decode ActiveSG transport response: %w", err)
	}
	if len(responses) != len(endpoints) {
		return nil, fmt.Errorf("ActiveSG transport returned %d responses for %d requests", len(responses), len(endpoints))
	}
	bodies := make([]json.RawMessage, 0, len(responses))
	for index, response := range responses {
		if response.Status < 200 || response.Status > 299 {
			return nil, fmt.Errorf("request %d: HTTP %d %s", index+1, response.Status, response.StatusText)
		}
		body := json.RawMessage(response.Body)
		if !json.Valid(body) {
			return nil, fmt.Errorf("request %d: response is not JSON", index+1)
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

func activeSGProcedureURL(baseURL, procedure string, input any, permittedHosts []string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse ActiveSG URL: %w", err)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode ActiveSG request: %w", err)
	}
	parsed.Path = "/api/trpc/" + procedure
	parsed.RawPath = ""
	parsed.RawQuery = url.Values{"input": []string{string(payload)}}.Encode()
	parsed.Fragment = ""
	if err := validateURL(parsed.String(), permittedHosts); err != nil {
		return "", fmt.Errorf("ActiveSG API URL: %w", err)
	}
	return parsed.String(), nil
}

func activeSGActivityID(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse ActiveSG venue list URL: %w", err)
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) != 4 || segments[0] != "facility-bookings" || segments[1] != "activities" || segments[2] == "" || segments[3] != "venues" {
		return "", errors.New("ActiveSG venue list URL must be a facility-bookings activity venue list")
	}
	return segments[2], nil
}

func activeSGTimeslotURL(baseURL, activityID, venueID string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse ActiveSG venue list URL: %w", err)
	}
	parsed.Path = "/facility-bookings/activities/" + url.PathEscape(activityID) + "/venues/" + url.PathEscape(venueID) + "/timeslots"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func validateActiveSGRequest(request ActiveSGScanRequest) error {
	if err := validateURL(request.VenueListURL, request.PermittedHosts); err != nil {
		return fmt.Errorf("ActiveSG venue list URL: %w", err)
	}
	if _, err := activeSGActivityID(request.VenueListURL); err != nil {
		return err
	}
	if strings.TrimSpace(request.SessionStateBase64) == "" {
		return errors.New("ActiveSG scan needs an imported browser session")
	}
	if !request.ScanAll && len(normalizedActiveSGNames(request.VenueNames)) == 0 {
		return errors.New("ActiveSG scan needs venue names or scan_all")
	}
	return nil
}

func normalizedActiveSGNames(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = canonicalActiveSGName(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func canonicalActiveSGName(value string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	for index, word := range words {
		if word == "sports" {
			words[index] = "sport"
		}
	}
	return strings.Join(words, " ")
}

func matchesActiveSGVenue(name string, requested []string, scanAll bool) bool {
	if scanAll {
		return true
	}
	name = canonicalActiveSGName(name)
	for _, value := range normalizedActiveSGNames(requested) {
		if strings.Contains(name, value) {
			return true
		}
	}
	return false
}
