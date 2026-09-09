package browser

import (
	"strings"
	"testing"
	"time"
)

func TestParsePerfectGymScheduleMapsNestedBookableSlots(t *testing.T) {
	body := []byte(`{
  "CalendarData": [{
    "Hour": "07:00",
    "ClassesPerDay": [
      [],
      [{"StartTime":"2026-09-01T07:00:00","EndTime":"2026-09-01T08:00:00","Status":"Bookable"}]
    ]
  }],
  "PagerData": {"CanGoForward": true}
}`)
	entries, canGoForward, err := parsePerfectGymSchedule(body)
	if err != nil {
		t.Fatal(err)
	}
	if !canGoForward || len(entries) != 1 || entries[0].Status != "Bookable" || entries[0].End.Sub(entries[0].Start) != time.Hour {
		t.Fatalf("entries = %#v, canGoForward = %t", entries, canGoForward)
	}
	if entries[0].Start.Location().String() != "Asia/Singapore" {
		t.Fatalf("start location = %s", entries[0].Start.Location())
	}
}

func TestParsePerfectGymScheduleRejectsInvalidSlot(t *testing.T) {
	body := []byte(`{"CalendarData":[{"ClassesPerDay":[[{"StartTime":"2026-09-01T08:00:00","EndTime":"2026-09-01T07:00:00","Status":"Bookable"}]]}],"PagerData":{}}`)
	if _, _, err := parsePerfectGymSchedule(body); err == nil || !strings.Contains(err.Error(), "ends before") {
		t.Fatalf("parsePerfectGymSchedule() error = %v", err)
	}
}

func TestPerfectGymRequestNeedsSessionAndFacilityType(t *testing.T) {
	request := PerfectGymScanRequest{AvailabilityURL: "https://thekallang.perfectgym.com/clientportal2/", PermittedHosts: []string{"thekallang.perfectgym.com"}}
	if err := validatePerfectGymRequest(request); err == nil {
		t.Fatal("request without session was accepted")
	}
	request.SessionStateBase64 = "state"
	if err := validatePerfectGymRequest(request); err == nil {
		t.Fatal("request without facility type was accepted")
	}
	request.FacilityTypeName = "Badminton Courts"
	if err := validatePerfectGymRequest(request); err != nil {
		t.Fatalf("valid request = %v", err)
	}
}

func TestPerfectGymScheduleResponseMatchesFacilityScopedPOST(t *testing.T) {
	url := "https://thekallang.perfectgym.com/clientportal2/FacilityBookings/FacilityCalendar/GetWeeklySchedule"
	for _, test := range []struct {
		name         string
		status       int
		method, body string
		want         bool
	}{
		{name: "facility scoped response", status: 200, method: "POST", body: `{"clubId":1,"zoneTypeId":31,"daysInWeek":6}`, want: true},
		{name: "empty bootstrap response", status: 200, method: "POST", body: `{"clubId":1,"daysInWeek":6}`, want: false},
		{name: "wrong status", status: 500, method: "POST", body: `{"zoneTypeId":31}`, want: false},
		{name: "wrong method", status: 200, method: "GET", body: `{"zoneTypeId":31}`, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := perfectGymScheduleResponseMatches(url, test.status, test.method, test.body); got != test.want {
				t.Fatalf("perfectGymScheduleResponseMatches() = %t, want %t", got, test.want)
			}
		})
	}
}
