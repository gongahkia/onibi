package query

import (
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/store"
)

func TestFilterComposesContiguousSlotsAndAppliesTotalPrice(t *testing.T) {
	now := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	firstPrice := int64(1000)
	secondPrice := int64(1200)
	available := func(id string, start time.Time, price *int64) store.SlotWithVenue {
		return store.SlotWithVenue{
			Slot:  domain.AvailabilitySlot{ID: id, VenueID: "venue", CourtName: "Court 1", SourceID: "local-manual", Start: start, End: start.Add(time.Hour), Status: domain.AvailabilityAvailable, PriceCents: price, Currency: "SGD", ObservedAt: now, FetchedAt: now, StaleAfter: now.Add(time.Hour), Provenance: domain.Provenance{SourceID: "local-manual"}},
			Venue: domain.Venue{ID: "venue", Name: "Venue", Coordinates: domain.Coordinates{Latitude: 1.3, Longitude: 103.8}},
		}
	}
	rows := []store.SlotWithVenue{available("one", now.Add(time.Hour), &firstPrice), available("two", now.Add(2*time.Hour), &secondPrice)}
	maximum := int64(2200)
	results, err := Filter(rows, domain.Query{MinimumDuration: 2 * time.Hour, MaximumPriceCents: &maximum}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Filter() length = %d, want 1", len(results))
	}
	if results[0].Slot.PriceCents == nil || *results[0].Slot.PriceCents != 2200 || len(results[0].ComponentSlotIDs) != 2 {
		t.Fatalf("composed result = %#v", results[0])
	}
	maximum = 2100
	results, err = Filter(rows, domain.Query{MinimumDuration: 2 * time.Hour, MaximumPriceCents: &maximum}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results with insufficient total price = %#v", results)
	}
}

func TestFilterRejectsStaleAndUnknownAttributeWhenRequested(t *testing.T) {
	now := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	indoor := true
	rows := []store.SlotWithVenue{
		{Slot: domain.AvailabilitySlot{ID: "fresh", VenueID: "one", SourceID: "local-manual", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Status: domain.AvailabilityAvailable, ObservedAt: now, FetchedAt: now, StaleAfter: now.Add(time.Hour)}, Venue: domain.Venue{ID: "one", Name: "one", Indoor: &indoor}},
		{Slot: domain.AvailabilitySlot{ID: "stale", VenueID: "two", SourceID: "local-manual", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Status: domain.AvailabilityAvailable, ObservedAt: now, FetchedAt: now, StaleAfter: now.Add(-time.Second)}, Venue: domain.Venue{ID: "two", Name: "two", Indoor: &indoor}},
	}
	results, err := Filter(rows, domain.Query{Indoor: &indoor}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Slot.ID != "fresh" {
		t.Fatalf("Filter() = %#v", results)
	}
	notIndoor := false
	results, err = Filter(rows, domain.Query{Indoor: &notIndoor}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("Filter() with false indoor = %#v", results)
	}
}
