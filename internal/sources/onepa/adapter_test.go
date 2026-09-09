package onepa

import (
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

func TestNormalizeMapsAvailableNamedCourts(t *testing.T) {
	adapter := testAdapter(t)
	response, err := parseResponse([]byte(`{
  "errorMessages": "",
  "hasError": false,
  "response": {
    "outletName": "Woodlands CC",
    "resourceList": [
      {
        "resourceId": "court-1",
        "resourceName": "Badminton Court 1",
        "slotList": [
          {
            "actualStatus": "Available",
            "availabilityStatus": "Available",
			"startTime": "2026-08-31T10:30",
            "endTime": "2026-08-31T11:30:00",
            "timeRangeId": "range-1",
            "isAvailable": true
          },
          {
            "actualStatus": "Reserved",
            "availabilityStatus": "Unavailable",
            "startTime": "2026-08-31T11:30:00",
            "endTime": "2026-08-31T12:30:00",
            "timeRangeId": "range-2",
            "isAvailable": false
          }
        ]
      },
      {
        "resourceId": "court-2",
        "resourceName": "Badminton Court 2",
        "slotList": [
          {
            "actualStatus": "Available",
            "availabilityStatus": "Available",
            "startTime": "2026-08-31T12:30:00+08:00",
            "endTime": "2026-08-31T13:30:00+08:00",
            "timeRangeId": "range-3",
            "isAvailable": true
          }
        ]
      }
    ]
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.August, 31, 0, 0, 0, 0, location)
	snapshot, err := adapter.normalize("WoodlandsCC_BADMINTONCOURTS", response, source.AvailabilityRequest{StartDate: start, EndDate: start.AddDate(0, 0, 1)}, time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Venues) != 1 || snapshot.Venues[0].Name != "Woodlands CC" {
		t.Fatalf("venues = %#v", snapshot.Venues)
	}
	if got := snapshot.Venues[0].SourceIDs; len(got) != 1 || got[0] != "WoodlandsCC_BADMINTONCOURTS" {
		t.Fatalf("venue source IDs = %#v", got)
	}
	if len(snapshot.Slots) != 2 {
		t.Fatalf("slots = %#v", snapshot.Slots)
	}
	if snapshot.Slots[0].CourtName != "Badminton Court 1" || snapshot.Slots[0].Status != domain.AvailabilityAvailable || snapshot.Slots[0].FacilityID != "" {
		t.Fatalf("first slot = %#v", snapshot.Slots[0])
	}
	if snapshot.Slots[1].CourtName != "Badminton Court 2" || snapshot.Slots[1].End.Sub(snapshot.Slots[1].Start) != time.Hour {
		t.Fatalf("second slot = %#v", snapshot.Slots[1])
	}
}

func TestNormalizeRejectsInvalidAvailableSlot(t *testing.T) {
	adapter := testAdapter(t)
	response, err := parseResponse([]byte(`{
  "hasError": false,
  "response": {
    "outletName": "Woodlands CC",
    "resourceList": [{
      "resourceId": "court-1",
      "resourceName": "Badminton Court 1",
      "slotList": [{
        "startTime": "not-a-time",
        "endTime": "2026-08-31T11:30:00",
        "timeRangeId": "range-1",
        "isAvailable": true
      }]
    }]
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.normalize("WoodlandsCC_BADMINTONCOURTS", response, source.AvailabilityRequest{}, time.Now().UTC()); err == nil {
		t.Fatal("invalid available slot was accepted")
	}
}

func TestParseResponseReturnsProviderError(t *testing.T) {
	if _, err := parseResponse([]byte(`{"hasError":true,"errorMessages":"Facility is not available"}`)); err == nil {
		t.Fatal("provider error was accepted")
	}
}

func TestParseResponseRejectsMissingPayload(t *testing.T) {
	if _, err := parseResponse([]byte(`{"hasError":false}`)); err == nil {
		t.Fatal("missing response payload was accepted")
	}
}

func TestOnePAResponseContentErrorClassifiesWAFPage(t *testing.T) {
	err := onePAResponseContentError("text/html", []byte("<html>Request unsuccessful. Incapsula incident ID</html>"))
	if err == nil || !strings.Contains(err.Error(), "Incapsula") {
		t.Fatalf("WAF error = %v", err)
	}
	if err := onePAResponseContentError("application/json; charset=utf-8", []byte(`{"hasError":false}`)); err != nil {
		t.Fatalf("JSON response error = %v", err)
	}
}

func TestAdapterRequiresConfiguredFacilityIDs(t *testing.T) {
	if err := validateSettings(config.SourceOnePA{Enabled: true}); err == nil {
		t.Fatal("unscoped onePA reader was accepted")
	}
	ids := facilityIDs(config.SourceOnePA{Enabled: true, FacilityIDs: []string{" WoodlandsCC_BADMINTONCOURTS ", "WoodlandsCC_BADMINTONCOURTS"}})
	if len(ids) != 1 || ids[0] != "WoodlandsCC_BADMINTONCOURTS" {
		t.Fatalf("facility IDs = %#v", ids)
	}
}

func TestRequestedDatesCoversEverySingaporeCalendarDay(t *testing.T) {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.August, 31, 0, 0, 0, 0, location)
	dates := requestedDates(source.AvailabilityRequest{StartDate: start.UTC(), EndDate: start.AddDate(0, 0, 7).UTC()})
	if len(dates) != 7 || dates[0].Format("2006-01-02") != "2026-08-31" || dates[1].Format("2006-01-02") != "2026-09-01" || dates[6].Format("2006-01-02") != "2026-09-06" {
		t.Fatalf("dates = %#v", dates)
	}
}

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	return &Adapter{info: domain.SourceInfo{ID: SourceID}, settings: config.SourceOnePA{Enabled: true, FacilityIDs: []string{"WoodlandsCC_BADMINTONCOURTS"}}, refreshMinutes: 30}
}
