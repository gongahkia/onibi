package browser

import (
	"strings"
	"testing"
	"time"
)

func TestActiveSGDateTypesAndAvailability(t *testing.T) {
	body := strings.Join([]string{
		"Select date & time",
		"Instant",
		"Tue", "25 Aug",
		"Thu", "27 Aug",
		"Ballot",
		"Sun", "6 Sep",
	}, "\n")
	types := activeSGDateTypes(body)
	if types["View timeslots for Tue, 25 Aug"] != ActiveSGInstant || types["View timeslots for Sun, 6 Sep"] != ActiveSGBallot {
		t.Fatalf("date types = %#v", types)
	}
	status, times := activeSGDateAvailability("Each slot is 1 hour long.\n7:00 am\n12:00 pm\n1:00 pm", ActiveSGInstant)
	if status != ActiveSGSlotsVisible || strings.Join(times, ",") != "07:00,12:00,13:00" {
		t.Fatalf("instant availability = %q, %#v", status, times)
	}
	status, times = activeSGDateAvailability("You've already balloted", ActiveSGBallot)
	if status != ActiveSGAlreadyBalloted || len(times) != 0 {
		t.Fatalf("ballot availability = %q, %#v", status, times)
	}
}

func TestActiveSGSlotStartTimesDoesNotTreatRangeEndAsAStart(t *testing.T) {
	times := activeSGSlotStartTimes("7:00 am - 8:00 am\n9:30 am\n12:00 pm")
	if strings.Join(times, ",") != "07:00,09:30,12:00" {
		t.Fatalf("slot times = %#v", times)
	}
}

func TestParseActiveSGDateCarriesJanuaryIntoNextYear(t *testing.T) {
	now := time.Date(2026, time.December, 20, 12, 0, 0, 0, time.FixedZone("SGT", 8*60*60))
	date, err := parseActiveSGDate("View timeslots for Fri, 2 Jan", now)
	if err != nil {
		t.Fatal(err)
	}
	if date.Format("2006-01-02") != "2027-01-02" || date.Location().String() != "Asia/Singapore" {
		t.Fatalf("date = %s (%s)", date, date.Location())
	}
}

func TestActiveSGRequestAndVenueMatchingRequireExplicitScope(t *testing.T) {
	request := ActiveSGScanRequest{VenueListURL: "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues", SessionStateBase64: "state", PermittedHosts: []string{"activesg.gov.sg"}}
	if err := validateActiveSGRequest(request); err == nil {
		t.Fatal("unscoped ActiveSG request was accepted")
	}
	request.VenueNames = []string{"jurong east"}
	if err := validateActiveSGRequest(request); err != nil {
		t.Fatalf("scoped ActiveSG request = %v", err)
	}
	if !matchesActiveSGVenue("Jurong East Sport Hall", request.VenueNames, false) || matchesActiveSGVenue("Bishan Sport Hall", request.VenueNames, false) {
		t.Fatal("venue name matching is incorrect")
	}
}
