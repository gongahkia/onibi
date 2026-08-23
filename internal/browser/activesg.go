package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	ActiveSGInstant          = "instant"
	ActiveSGBallot           = "ballot"
	ActiveSGSlotsVisible     = "slots_visible"
	ActiveSGBallotAvailable  = "ballot_available"
	ActiveSGAlreadyBalloted  = "already_balloted"
	ActiveSGNoHourlySlots    = "no_hourly_slots_visible"
	activeSGDateButtonSelect = `button[aria-label^="View timeslots for "]`
	activeSGVenueLinkSelect  = `a[href*="/venues/"][href$="/timeslots"]`
)

var (
	activeSGDateLabelPattern = regexp.MustCompile(`^View timeslots for (Mon|Tue|Wed|Thu|Fri|Sat|Sun), (\d{1,2}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$`)
	activeSGTimePattern      = regexp.MustCompile(`(?i)\b(1[0-2]|0?[1-9]):([0-5][0-9])\s*(am|pm)\b`)
	activeSGWeekdayPattern   = regexp.MustCompile(`^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)$`)
	activeSGMonthDayPattern  = regexp.MustCompile(`^\d{1,2} (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$`)
)

// ActiveSGScanRequest scopes a read-only browser scan to a venue-list page.
// The imported session is used in memory only; this path contains no login or
// booking action.
type ActiveSGScanRequest struct {
	VenueListURL       string
	VenueNames         []string
	ScanAll            bool
	SessionStateBase64 string
	PermittedHosts     []string
	Timeout            time.Duration
}

// ActiveSGAvailability is the observed venue-level date result before it is
// mapped into Kaypoh's bookable-slot model. ActiveSG does not expose court IDs
// on this surface, so SlotStartTimes are aggregate venue-level starts.
type ActiveSGAvailability struct {
	VenueID            string
	VenueName          string
	VenueURL           string
	Date               time.Time
	DateLabel          string
	AvailabilityType   string
	AvailabilityStatus string
	SlotStartTimes     []string
}

// ActiveSGScanner keeps the provider adapter testable without launching
// Chromium. It has no method capable of booking or reviewing a ballot.
type ActiveSGScanner interface {
	ScanActiveSG(context.Context, ActiveSGScanRequest) ([]ActiveSGAvailability, error)
}

// ScanActiveSG reads the currently listed ActiveSG venues sequentially. It
// opens venue pages and clicks date cards only; it never selects a slot or
// follows a Continue, Review ballot, checkout, or payment control.
func (client *Playwright) ScanActiveSG(ctx context.Context, request ActiveSGScanRequest) ([]ActiveSGAvailability, error) {
	if err := validateActiveSGRequest(request); err != nil {
		return nil, err
	}
	if err := acquire(ctx, client.sem); err != nil {
		return nil, err
	}
	defer func() { <-client.sem }()
	if err := client.start(); err != nil {
		return nil, err
	}

	options, err := contextOptions(request.SessionStateBase64)
	if err != nil {
		return nil, err
	}
	options.TimezoneId = playwright.String("Asia/Singapore")
	context, err := client.browser.NewContext(options)
	if err != nil {
		return nil, fmt.Errorf("create ActiveSG browser context: %w", err)
	}
	defer context.Close()
	page, err := context.NewPage()
	if err != nil {
		return nil, fmt.Errorf("open ActiveSG venue list: %w", err)
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	page.SetDefaultTimeout(float64(timeout.Milliseconds()))
	page.SetDefaultNavigationTimeout(float64(timeout.Milliseconds()))

	venues, err := activeSGVenueLinks(ctx, page, request)
	if err != nil {
		return nil, err
	}
	results := make([]ActiveSGAvailability, 0, len(venues)*4)
	for _, venue := range venues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := activeSGVenueDates(ctx, page, venue)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
	}
	return results, nil
}

type activeSGVenueLink struct {
	ID   string
	Name string
	URL  string
}

func validateActiveSGRequest(request ActiveSGScanRequest) error {
	if err := validateURL(request.VenueListURL, request.PermittedHosts); err != nil {
		return fmt.Errorf("ActiveSG venue list URL: %w", err)
	}
	parsed, _ := url.Parse(request.VenueListURL)
	if !strings.HasPrefix(parsed.Path, "/facility-bookings/activities/") || !strings.HasSuffix(parsed.Path, "/venues") {
		return errors.New("ActiveSG venue list URL must be a facility-bookings activity venue list")
	}
	if strings.TrimSpace(request.SessionStateBase64) == "" {
		return errors.New("ActiveSG scan needs an imported browser session")
	}
	if !request.ScanAll && len(normalizedActiveSGNames(request.VenueNames)) == 0 {
		return errors.New("ActiveSG scan needs venue names or scan_all")
	}
	return nil
}

func activeSGVenueLinks(ctx context.Context, page playwright.Page, request ActiveSGScanRequest) ([]activeSGVenueLink, error) {
	if _, err := page.Goto(request.VenueListURL); err != nil {
		return nil, fmt.Errorf("open ActiveSG venue list: %w", err)
	}
	if _, err := page.WaitForSelector(activeSGVenueLinkSelect); err != nil {
		return nil, fmt.Errorf("wait for ActiveSG venue list: %w", err)
	}
	links, err := page.Locator(activeSGVenueLinkSelect).All()
	if err != nil {
		return nil, fmt.Errorf("read ActiveSG venue list: %w", err)
	}
	venues := make([]activeSGVenueLink, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		href, err := link.GetAttribute("href")
		if err != nil || strings.TrimSpace(href) == "" {
			return nil, fmt.Errorf("read ActiveSG venue link: %w", err)
		}
		name, err := link.InnerText()
		if err != nil {
			return nil, fmt.Errorf("read ActiveSG venue name: %w", err)
		}
		name = firstActiveSGLine(name)
		venueURL, venueID, err := activeSGTimeslotURL(request.VenueListURL, href, request.PermittedHosts)
		if err != nil {
			return nil, err
		}
		if name == "" || !matchesActiveSGVenue(name, request.VenueNames, request.ScanAll) {
			continue
		}
		if _, ok := seen[venueID]; ok {
			continue
		}
		seen[venueID] = struct{}{}
		venues = append(venues, activeSGVenueLink{ID: venueID, Name: name, URL: venueURL})
	}
	if len(venues) == 0 {
		return nil, errors.New("ActiveSG venue list did not match any requested venue")
	}
	return venues, nil
}

func activeSGTimeslotURL(baseURL, href string, permittedHosts []string) (string, string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("parse ActiveSG venue list URL: %w", err)
	}
	reference, err := url.Parse(href)
	if err != nil {
		return "", "", fmt.Errorf("parse ActiveSG venue link: %w", err)
	}
	resolved := base.ResolveReference(reference)
	if err := validateURL(resolved.String(), permittedHosts); err != nil {
		return "", "", fmt.Errorf("ActiveSG venue link: %w", err)
	}
	segments := strings.Split(strings.Trim(resolved.Path, "/"), "/")
	venueID := ""
	for index, segment := range segments {
		if segment == "venues" && index+2 < len(segments) && segments[index+2] == "timeslots" {
			venueID = segments[index+1]
			break
		}
	}
	if venueID == "" {
		return "", "", errors.New("ActiveSG venue link is not a timeslots page")
	}
	return resolved.String(), venueID, nil
}

func activeSGVenueDates(ctx context.Context, page playwright.Page, venue activeSGVenueLink) ([]ActiveSGAvailability, error) {
	if _, err := page.Goto(venue.URL); err != nil {
		return nil, fmt.Errorf("open ActiveSG venue %q: %w", venue.Name, err)
	}
	if _, err := page.WaitForSelector(activeSGDateButtonSelect); err != nil {
		return nil, fmt.Errorf("wait for ActiveSG dates at %q: %w", venue.Name, err)
	}
	before, err := page.Locator("body").InnerText()
	if err != nil {
		return nil, fmt.Errorf("read ActiveSG dates at %q: %w", venue.Name, err)
	}
	types := activeSGDateTypes(before)
	buttons, err := page.Locator(activeSGDateButtonSelect).All()
	if err != nil {
		return nil, fmt.Errorf("read ActiveSG date controls at %q: %w", venue.Name, err)
	}
	results := make([]ActiveSGAvailability, 0, len(buttons))
	seenDates := make(map[string]struct{}, len(buttons))
	for _, button := range buttons {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		label, err := button.GetAttribute("aria-label")
		if err != nil {
			return nil, fmt.Errorf("read ActiveSG date label at %q: %w", venue.Name, err)
		}
		date, err := parseActiveSGDate(label, time.Now())
		if err != nil {
			return nil, fmt.Errorf("parse ActiveSG date at %q: %w", venue.Name, err)
		}
		key := date.Format("2006-01-02")
		if _, ok := seenDates[key]; ok {
			continue
		}
		seenDates[key] = struct{}{}
		if err := button.Click(); err != nil {
			return nil, fmt.Errorf("select ActiveSG date %q at %q: %w", label, venue.Name, err)
		}
		page.WaitForTimeout(250)
		body, err := page.Locator("body").InnerText()
		if err != nil {
			return nil, fmt.Errorf("read ActiveSG date %q at %q: %w", label, venue.Name, err)
		}
		availabilityType := types[label]
		status, times := activeSGDateAvailability(body, availabilityType)
		if availabilityType == "" {
			availabilityType = activeSGTypeFromStatus(status, times)
		}
		results = append(results, ActiveSGAvailability{
			VenueID: venue.ID, VenueName: venue.Name, VenueURL: venue.URL, Date: date, DateLabel: strings.TrimPrefix(label, "View timeslots for "),
			AvailabilityType: availabilityType, AvailabilityStatus: status, SlotStartTimes: times,
		})
	}
	return results, nil
}

func activeSGDateTypes(body string) map[string]string {
	result := make(map[string]string)
	lines := activeSGLines(body)
	availabilityType := ""
	for index := 0; index+1 < len(lines); index++ {
		switch strings.ToLower(lines[index]) {
		case ActiveSGInstant:
			availabilityType = ActiveSGInstant
			continue
		case ActiveSGBallot:
			availabilityType = ActiveSGBallot
			continue
		}
		if availabilityType == "" || !activeSGWeekdayPattern.MatchString(lines[index]) || !activeSGMonthDayPattern.MatchString(lines[index+1]) {
			continue
		}
		result["View timeslots for "+lines[index]+", "+lines[index+1]] = availabilityType
	}
	return result
}

func activeSGDateAvailability(body, availabilityType string) (string, []string) {
	lower := strings.ToLower(strings.ReplaceAll(body, "’", "'"))
	if strings.Contains(lower, "you've already balloted") {
		return ActiveSGAlreadyBalloted, nil
	}
	if availabilityType == ActiveSGBallot || strings.Contains(lower, "review ballot") {
		return ActiveSGBallotAvailable, nil
	}
	times := activeSGSlotStartTimes(body)
	if len(times) > 0 {
		return ActiveSGSlotsVisible, times
	}
	return ActiveSGNoHourlySlots, nil
}

func activeSGTypeFromStatus(status string, times []string) string {
	if status == ActiveSGBallotAvailable || status == ActiveSGAlreadyBalloted {
		return ActiveSGBallot
	}
	if len(times) > 0 {
		return ActiveSGInstant
	}
	return ""
}

func activeSGSlotStartTimes(body string) []string {
	seen := make(map[string]struct{})
	for _, line := range activeSGLines(body) {
		matches := activeSGTimePattern.FindAllStringSubmatch(strings.ToLower(line), -1)
		for index, match := range matches {
			if len(matches) > 1 && strings.Contains(line, "-") && index > 0 {
				break
			}
			hour, _ := strconv.Atoi(match[1])
			minute, _ := strconv.Atoi(match[2])
			if match[3] == "am" && hour == 12 {
				hour = 0
			}
			if match[3] == "pm" && hour != 12 {
				hour += 12
			}
			seen[fmt.Sprintf("%02d:%02d", hour, minute)] = struct{}{}
		}
	}
	times := make([]string, 0, len(seen))
	for value := range seen {
		times = append(times, value)
	}
	sort.Strings(times)
	return times
}

func parseActiveSGDate(label string, now time.Time) (time.Time, error) {
	matches := activeSGDateLabelPattern.FindStringSubmatch(strings.TrimSpace(label))
	if matches == nil {
		return time.Time{}, fmt.Errorf("unexpected date label %q", label)
	}
	location, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	localNow := now.In(location)
	day, _ := strconv.Atoi(matches[2])
	parsed, err := time.ParseInLocation("2 Jan 2006", fmt.Sprintf("%d %s %d", day, matches[3], localNow.Year()), location)
	if err != nil {
		return time.Time{}, err
	}
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	if parsed.Before(today.AddDate(0, 0, -1)) {
		parsed = parsed.AddDate(1, 0, 0)
	}
	return parsed, nil
}

func activeSGLines(body string) []string {
	parts := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func normalizedActiveSGNames(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func matchesActiveSGVenue(name string, requested []string, scanAll bool) bool {
	if scanAll {
		return true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	for _, value := range normalizedActiveSGNames(requested) {
		if strings.Contains(name, value) {
			return true
		}
	}
	return false
}

func firstActiveSGLine(value string) string {
	for _, line := range activeSGLines(value) {
		return line
	}
	return ""
}
