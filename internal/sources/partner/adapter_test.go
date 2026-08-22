package partner

import (
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/source"
)

func TestDecodeSnapshotNormalizesPartnerSlots(t *testing.T) {
	fetchedAt := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	request := source.AvailabilityRequest{StartDate: time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)}
	body := []byte(`{"slots":[{"id":"slot-42","venue_id":"guillemard","venue_name":"KFF Badminton Arena","court_id":"court-3","court_name":"Court 3","start_at":"2026-08-23T19:00:00+08:00","end_at":"2026-08-23T20:00:00+08:00","price_cents":1400,"currency":"SGD","booking_url":"https://booking.example.test/court-3"}]}`)
	snapshot, err := decodeSnapshot(body, "sba-stadium", request, fetchedAt, fetchedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Venues) != 1 || len(snapshot.Slots) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	slot := snapshot.Slots[0]
	if slot.ID != "sba-stadium:slot:slot-42" || slot.VenueID != "sba-stadium:venue:guillemard" || slot.CourtName != "Court 3" {
		t.Fatalf("slot identity = %#v", slot)
	}
	if slot.SourceID != "sba-stadium" || slot.Provenance.SourceID != "sba-stadium" || !slot.Start.Equal(time.Date(2026, time.August, 23, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("slot provenance/time = %#v", slot)
	}
	if snapshot.Venues[0].Name != "KFF Badminton Arena" || snapshot.Venues[0].Provenance.SourceReference != "guillemard" {
		t.Fatalf("venue = %#v", snapshot.Venues[0])
	}
}

func TestDecodeSnapshotAcceptsEmptyAvailabilityAndDropsOutOfRangeSlots(t *testing.T) {
	now := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	request := source.AvailabilityRequest{StartDate: time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)}
	snapshot, err := decodeSnapshot([]byte(`{"availability":[]}`), "onepa", request, now, now.Add(time.Hour))
	if err != nil || len(snapshot.Slots) != 0 || len(snapshot.Venues) != 0 {
		t.Fatalf("empty snapshot = %#v, %v", snapshot, err)
	}
	snapshot, err = decodeSnapshot([]byte(`[{"venue_id":"cc","start":"2026-08-25T10:00:00+08:00","end":"2026-08-25T11:00:00+08:00"}]`), "onepa", request, now, now.Add(time.Hour))
	if err != nil || len(snapshot.Slots) != 0 || len(snapshot.Venues) != 0 {
		t.Fatalf("out-of-range snapshot = %#v, %v", snapshot, err)
	}
}

func TestEndpointURLAndBrowserWindowExpansion(t *testing.T) {
	request := source.AvailabilityRequest{StartDate: time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC)}
	endpoint, err := endpointURL("https://partner.example/api", "/v1/availability?venue=one", request)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://partner.example/api/v1/availability?end_date=2026-08-30&start_date=2026-08-23&venue=one" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	got := expandAvailabilityURL("https://partner.example/calendar?from={start_date}&to={end_date}", request)
	if got != "https://partner.example/calendar?from=2026-08-23&to=2026-08-30" {
		t.Fatalf("expanded browser URL = %q", got)
	}
}

func TestDecodeVenuesAcceptsSeparatePartnerCatalogue(t *testing.T) {
	now := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	venues, err := decodeVenues([]byte(`{"venues":[{"id":"sims","name":"SBH Sims","address":"Sims Avenue","latitude":1.31,"longitude":103.88}]}`), "singapore-badminton-hall", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(venues) != 1 || venues[0].ID != "singapore-badminton-hall:venue:sims" || venues[0].Name != "SBH Sims" {
		t.Fatalf("venues = %#v", venues)
	}
}
