package browser

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const activeSGTestVenueListURL = "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"

func TestDecodeActiveSGScheduleFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/activesg_schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope activeSGEnvelope[[]json.RawMessage]
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	venue := activeSGVenue{ID: "OzrxvbMIJ0qQEw9h0suQT", Name: "Woodlands Sport Hall", Address: "2 Woodlands Street 12", PostalCode: "738620", Latitude: 1.4341, Longitude: 103.7798}
	rows, err := decodeActiveSGSchedule(envelope.Result.Data.JSON, activeSGTestVenueListURL, "YLONatwvqJfikKOmB5N9U", venue)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].AvailabilityStatus != ActiveSGSlotsVisible || rows[1].AvailabilityStatus != ActiveSGBallotAvailable {
		t.Fatalf("rows = %#v", rows)
	}
	if len(rows[0].Slots) != 1 || rows[0].Slots[0].Start.In(rows[0].Date.Location()).Format("15:04") != "07:00" || rows[0].Slots[0].End.Sub(rows[0].Slots[0].Start).Hours() != 1 {
		t.Fatalf("instant slots = %#v", rows[0].Slots)
	}
	if strings.Join(rows[0].Slots[0].SubvenueIDs, ",") != "court-a,court-b" {
		t.Fatalf("subvenues = %#v", rows[0].Slots[0].SubvenueIDs)
	}
	if rows[0].VenuePostalCode != "738620" || !strings.Contains(rows[0].VenueURL, "/OzrxvbMIJ0qQEw9h0suQT/timeslots") {
		t.Fatalf("venue metadata = %#v", rows[0])
	}
}

func TestActiveSGVenueFixtureAndCommonNameAlias(t *testing.T) {
	body, err := os.ReadFile("testdata/activesg_venues.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope activeSGEnvelope[[]activeSGVenue]
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Result.Data.JSON) != 1 || !matchesActiveSGVenue(envelope.Result.Data.JSON[0].Name, []string{"Woodlands Sports Hall"}, false) {
		t.Fatalf("venues = %#v", envelope.Result.Data.JSON)
	}
	if matchesActiveSGVenue("Woodlands Secondary School Hall", []string{"Woodlands Sports Hall"}, false) {
		t.Fatal("common-name alias matched the wrong Woodlands venue")
	}
}

func TestActiveSGRequestValidationAndProcedureURL(t *testing.T) {
	request := ActiveSGScanRequest{VenueListURL: activeSGTestVenueListURL, SessionStateBase64: "state", PermittedHosts: []string{"activesg.gov.sg"}}
	if err := validateActiveSGRequest(request); err == nil {
		t.Fatal("unscoped ActiveSG request was accepted")
	}
	request.VenueNames = []string{"Woodlands Sports Hall"}
	if err := validateActiveSGRequest(request); err != nil {
		t.Fatalf("valid request = %v", err)
	}
	endpoint, err := activeSGProcedureURL(request.VenueListURL, activeSGScheduleProcedure, map[string]any{"json": map[string]string{"venueId": "venue"}}, request.PermittedHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(endpoint, "https://activesg.gov.sg/api/trpc/schedule.listAvailable?input=") {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestDecodeActiveSGScheduleRejectsUnknownShape(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`["2026-09-15",{"type":"mystery","timeslots":[]}]`)}
	_, err := decodeActiveSGSchedule(raw, activeSGTestVenueListURL, "YLONatwvqJfikKOmB5N9U", activeSGVenue{ID: "venue", Name: "Venue"})
	if err == nil || !strings.Contains(err.Error(), "unknown availability type") {
		t.Fatalf("error = %v", err)
	}
}
