package activesg

import (
	"context"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/browser"
	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

type scannerStub struct {
	rows    []browser.ActiveSGAvailability
	request browser.ActiveSGScanRequest
	err     error
}

func (stub *scannerStub) ScanActiveSG(_ context.Context, request browser.ActiveSGScanRequest) ([]browser.ActiveSGAvailability, error) {
	stub.request = request
	return stub.rows, stub.err
}

func TestAdapterNormalizesOnlyInstantHourlySlots(t *testing.T) {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, time.August, 25, 0, 0, 0, 0, location)
	stub := &scannerStub{rows: []browser.ActiveSGAvailability{
		{VenueID: "venue-1", VenueName: "Jurong East Sport Hall", VenueURL: "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues/venue-1/timeslots", Date: date, DateLabel: "Tue, 25 Aug", AvailabilityType: browser.ActiveSGInstant, AvailabilityStatus: browser.ActiveSGSlotsVisible, SlotStartTimes: []string{"07:00", "12:00"}},
		{VenueID: "venue-1", VenueName: "Jurong East Sport Hall", VenueURL: "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues/venue-1/timeslots", Date: date.AddDate(0, 0, 1), DateLabel: "Wed, 26 Aug", AvailabilityType: browser.ActiveSGBallot, AvailabilityStatus: browser.ActiveSGBallotAvailable},
	}}
	adapter := testAdapter(t, stub)
	request := source.AvailabilityRequest{StartDate: date.UTC(), EndDate: date.AddDate(0, 0, 2).UTC()}
	snapshot, err := adapter.FetchSnapshot(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Venues) != 1 || len(snapshot.Slots) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Venues[0].ID != "myactivesg:venue:venue-1" || snapshot.Venues[0].Name != "Jurong East Sport Hall" {
		t.Fatalf("venue = %#v", snapshot.Venues[0])
	}
	if snapshot.Slots[0].Status != domain.AvailabilityAvailable || snapshot.Slots[0].CourtName != "" || snapshot.Slots[0].End.Sub(snapshot.Slots[0].Start) != time.Hour {
		t.Fatalf("slot = %#v", snapshot.Slots[0])
	}
	if stub.request.VenueListURL != BadmintonVenueListURL || stub.request.ScanAll || len(stub.request.VenueNames) != 1 {
		t.Fatalf("scan request = %#v", stub.request)
	}
	health, err := adapter.Health(context.Background())
	if err != nil || health.State != domain.HealthHealthy || health.RecordsParsed != 2 {
		t.Fatalf("health = %#v, %v", health, err)
	}
}

func TestAdapterRejectsOtherActivityAndUnrecognizedResults(t *testing.T) {
	settings := config.Source{Enabled: true, ActiveSG: config.SourceActiveSG{Enabled: true, VenueListURL: "https://activesg.gov.sg/facility-bookings/activities/BPQihVHITc7IPGorVeB2Y/venues", VenueNames: []string{"Venue"}, SessionStateBase64: "state"}}
	if _, err := New(testInfo(), settings, &scannerStub{}); err == nil {
		t.Fatal("pickleball URL was accepted by badminton-only reader")
	}
	adapter := testAdapter(t, &scannerStub{rows: []browser.ActiveSGAvailability{{VenueID: "venue", VenueName: "Venue", VenueURL: "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues/venue/timeslots", Date: time.Now(), AvailabilityType: browser.ActiveSGInstant, AvailabilityStatus: "unexpected"}}})
	if _, err := adapter.FetchSnapshot(context.Background(), source.AvailabilityRequest{}); err == nil {
		t.Fatal("unrecognized ActiveSG result was accepted")
	}
}

func testAdapter(t *testing.T, scanner browser.ActiveSGScanner) *Adapter {
	t.Helper()
	settings := config.Source{
		Enabled: true, RefreshMinutes: 60,
		ActiveSG: config.SourceActiveSG{Enabled: true, VenueListURL: BadmintonVenueListURL, VenueNames: []string{"Jurong East"}, SessionStateBase64: "state"},
	}
	adapter, err := New(testInfo(), settings, scanner)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testInfo() domain.SourceInfo {
	return domain.SourceInfo{ID: SourceID, Name: "ActiveSG", Operator: "Sport Singapore", Policy: domain.SourcePolicy{PermittedHosts: []string{"activesg.gov.sg"}, Timeout: time.Second}}
}
