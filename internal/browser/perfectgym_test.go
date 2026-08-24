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
