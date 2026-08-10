package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

func TestUpsertAndListAvailabilityRetainsVenueAttributes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kaypoh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertSports(ctx, domain.Sports()); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSources(ctx, []domain.SourceInfo{{ID: "local-manual", Name: "Local manual input", Operator: "kaypoh user", Policy: domain.SourcePolicy{Status: domain.SourceManualOnly}}}, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	indoor := true
	sheltered := false
	now := time.Now().UTC().Truncate(time.Second)
	venue := domain.Venue{
		ID: "venue", Name: "Delta Sport Centre", Address: "900 Tiong Bahru Road", Sports: []string{"badminton"},
		Coordinates: domain.Coordinates{Latitude: 1.289, Longitude: 103.82}, Indoor: &indoor, Sheltered: &sheltered,
		BookingURLs: []string{"https://example.test/book"}, Provenance: domain.Provenance{SourceID: "local-manual", FetchedAt: now},
	}
	if err := store.UpsertVenues(ctx, []domain.Venue{venue}); err != nil {
		t.Fatal(err)
	}
	price := int64(1200)
	slot := domain.AvailabilitySlot{
		ID: "slot", SportID: "badminton", VenueID: venue.ID, SourceID: "local-manual", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour),
		Status: domain.AvailabilityAvailable, PriceCents: &price, Currency: "SGD", ObservedAt: now, FetchedAt: now, StaleAfter: now.Add(24 * time.Hour),
		Provenance: domain.Provenance{SourceID: "local-manual", FetchedAt: now},
	}
	if err := store.UpsertAvailability(ctx, []domain.AvailabilitySlot{slot}); err != nil {
		t.Fatal(err)
	}
	results, err := store.ListAvailability(ctx, now, now.Add(3*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("ListAvailability() length = %d, want 1", len(results))
	}
	got := results[0]
	if got.Slot.PriceCents == nil || *got.Slot.PriceCents != price {
		t.Fatalf("price = %v, want %d", got.Slot.PriceCents, price)
	}
	if got.Venue.Coordinates != venue.Coordinates || got.Venue.Indoor == nil || !*got.Venue.Indoor || got.Venue.Sheltered == nil || *got.Venue.Sheltered {
		t.Fatalf("venue attributes = %#v", got.Venue)
	}
	if len(got.Venue.BookingURLs) != 1 || got.Venue.BookingURLs[0] != venue.BookingURLs[0] {
		t.Fatalf("booking URLs = %#v", got.Venue.BookingURLs)
	}
}
