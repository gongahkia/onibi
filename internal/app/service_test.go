package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/domain"
	"github.com/gongahkia/courtsg/internal/geo"
	"github.com/gongahkia/courtsg/internal/source"
)

func TestOpenSeedsSourcePolicy(t *testing.T) {
	config, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	config.DatabasePath = filepath.Join(t.TempDir(), "courtsg.db")
	service, err := Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	record, err := service.Source(context.Background(), "safra")
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled {
		t.Fatal("SAFRA must not be enabled")
	}
	if _, err := service.SetSourceEnabled(context.Background(), "safra", true); !errors.Is(err, source.ErrPolicyDisabled) {
		t.Fatalf("SetSourceEnabled(safra) = %v, want policy error", err)
	}
}

func TestRouteFallsBackWithoutOptionalOneMapCredentials(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "courtsg.db")
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	route, err := service.Route(context.Background(), domain.Coordinates{Latitude: 1.3, Longitude: 103.8}, domain.Coordinates{Latitude: 1.31, Longitude: 103.81}, geo.ModeWalk)
	if err != nil {
		t.Fatal(err)
	}
	if !route.Fallback || service.RoutingStatus() == "configured" {
		t.Fatalf("expected Haversine fallback, got route %#v and state %q", route, service.RoutingStatus())
	}
}

func TestImportManualAvailabilityAndSearch(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "courtsg.db")
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	if err := service.store.UpsertVenues(context.Background(), []domain.Venue{{
		ID: "venue", Name: "Venue", Sports: []string{"badminton"}, Coordinates: domain.Coordinates{Latitude: 1.3, Longitude: 103.8},
		Provenance: domain.Provenance{SourceID: "local-manual", FetchedAt: now},
	}}); err != nil {
		t.Fatal(err)
	}
	price := int64(1600)
	if _, err := service.ImportManualAvailability(context.Background(), []domain.AvailabilitySlot{{
		SportID: "badminton", VenueID: "venue", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), PriceCents: &price,
	}}); err != nil {
		t.Fatal(err)
	}
	results, err := service.Search(context.Background(), domain.Query{Sports: []string{"shuttle"}, MinimumDuration: time.Hour, Ranking: domain.RankCheap})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Slot.SourceID != "local-manual" || results[0].Breakdown.CourtPriceCents == nil {
		t.Fatalf("Search() = %#v", results)
	}
}
