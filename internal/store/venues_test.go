package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
)

func TestUpsertAndSearchVenues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "courtsg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertSports(ctx, domain.Sports()); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSources(ctx, []domain.SourceInfo{{ID: "source", Name: "Source", Operator: "Operator", Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData}}}, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	venue := domain.Venue{ID: "venue", SourceIDs: []string{"upstream"}, Name: "Delta Sport Centre", Address: "900 Tiong Bahru Road", Sports: []string{"badminton"}, Coordinates: domain.Coordinates{Latitude: 1.289, Longitude: 103.82}, Provenance: domain.Provenance{SourceID: "source", FetchedAt: time.Now()}}
	if err := store.UpsertVenues(ctx, []domain.Venue{venue}); err != nil {
		t.Fatal(err)
	}
	venues, err := store.SearchVenues(ctx, VenueFilter{Search: "delta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(venues) != 1 || venues[0].ID != "venue" {
		t.Fatalf("SearchVenues() = %#v", venues)
	}
}
