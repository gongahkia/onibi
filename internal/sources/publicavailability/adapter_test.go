package publicavailability

import (
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

func TestSmashFragmentParsing(t *testing.T) {
	times := `<span class="available avail_slot" data-id="time_8">8am</span><span class="unavailable" data-id="time_9">9am</span>`
	ids := smashAvailableIDs(times, "avail_slot")
	if len(ids) != 1 || ids[0] != "time_8" {
		t.Fatalf("smashAvailableIDs() = %#v", ids)
	}
	courts := smashAvailableCourts(`<span class="available2" data-id="court_1_8">Court <b>1</b></span><span class="unavailable2" data-id="court_2_8">Court 2</span>`)
	if len(courts) != 1 || courts[0].ID != "court_1_8" || courts[0].Name != "Court 1" {
		t.Fatalf("smashAvailableCourts() = %#v", courts)
	}
	hour, err := smashHour("time_24")
	if err != nil || hour != 24 {
		t.Fatalf("smashHour() = %d, %v", hour, err)
	}
}

func TestLocalDateTimeSupportsProviderDayPrefix(t *testing.T) {
	parsed, err := localDateTime("2026-08-28", "1.00:00:00")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.August, 29, 0, 0, 0, 0, singapore()).UTC()
	if !parsed.Equal(want) {
		t.Fatalf("localDateTime() = %s, want %s", parsed, want)
	}
}

func TestSingaporeDatesAndRequestBounds(t *testing.T) {
	location := singapore()
	request := source.AvailabilityRequest{
		StartDate: time.Date(2026, time.August, 22, 0, 0, 0, 0, location).UTC(),
		EndDate:   time.Date(2026, time.August, 24, 0, 0, 0, 0, location).UTC(),
	}
	dates := singaporeDates(request)
	if len(dates) != 2 || dates[0].Format("2006-01-02") != "2026-08-22" || dates[1].Format("2006-01-02") != "2026-08-23" {
		t.Fatalf("singaporeDates() = %#v", dates)
	}
	start := time.Date(2026, time.August, 24, 0, 0, 0, 0, location).UTC()
	if withinRequest(start, start.Add(time.Hour), request) {
		t.Fatal("slot at exclusive end must not be included")
	}
}

func TestPriceAndCoordinateParsing(t *testing.T) {
	price := parsePriceCents("24 SGD")
	if price == nil || *price != 2400 {
		t.Fatalf("parsePriceCents() = %v", price)
	}
	if _, _, ok := wyseCoordinates("1.3326088", "103.7449928"); !ok {
		t.Fatal("expected valid Wyse coordinates")
	}
	if _, _, ok := wyseCoordinates("not-a-number", "103.7449928"); ok {
		t.Fatal("invalid coordinates were accepted")
	}
}

func TestSupportedSources(t *testing.T) {
	for _, sourceID := range []string{"sba-stadium", "singapore-badminton-hall", "smash-arena", "wyse-active"} {
		if !Supports(sourceID) {
			t.Fatalf("Supports(%q) = false", sourceID)
		}
	}
	if Supports("onepa") {
		t.Fatal("onePA must not claim a built-in reader")
	}
	if domain.SingaporeTimeZone != "Asia/Singapore" {
		t.Fatal("unexpected Singapore time zone")
	}
}
