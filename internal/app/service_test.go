package app

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/geo"
	"github.com/gongahkia/kaypoh/internal/sources/activesg"
	"github.com/gongahkia/kaypoh/internal/sources/fallback"
	"github.com/gongahkia/kaypoh/internal/sources/onepa"
	"github.com/gongahkia/kaypoh/internal/sources/perfectgym"
)

func TestOpenSeedsSourcePolicy(t *testing.T) {
	config, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	config.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	service, err := Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	record, err := service.Source(context.Background(), "onepa")
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled {
		t.Fatal("onePA must not be enabled without explicit configuration")
	}
	if _, err := service.SetSourceEnabled(context.Background(), "onepa", true); err != nil {
		t.Fatalf("SetSourceEnabled(onepa) = %v", err)
	}
}

func TestDisabledPartnerDoesNotResolveItsSecrets(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	cfg.Sources["onepa"] = config.Source{Browser: config.SourceBrowser{Enabled: true, SessionStateBase64: "env:KAYPOH_MISSING_SESSION"}}
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
}

func TestOpenSelectsDedicatedActiveSGReader(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	activeSG := cfg.Sources[activesg.SourceID]
	activeSG.Enabled = true
	activeSG.ActiveSG = config.SourceActiveSG{
		Enabled: true, VenueListURL: activesg.BadmintonVenueListURL, VenueNames: []string{"Jurong East"},
		SessionStateBase64: base64.StdEncoding.EncodeToString([]byte(`{"cookies":[],"origins":[]}`)),
	}
	cfg.Sources[activesg.SourceID] = activeSG
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	adapter, err := service.sources.Adapter(activesg.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*activesg.Adapter); !ok {
		t.Fatalf("myactivesg adapter = %T, want dedicated ActiveSG reader", adapter)
	}
}

func TestOpenChainsGenericAndDedicatedActiveSGReaders(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	activeSG := cfg.Sources[activesg.SourceID]
	activeSG.Enabled = true
	activeSG.Public = config.SourcePublic{Enabled: true, AvailabilityURL: "https://activesg.gov.sg/availability", SlotJSONSelector: "script#kaypoh-slots"}
	activeSG.ActiveSG = config.SourceActiveSG{
		Enabled: true, VenueListURL: activesg.BadmintonVenueListURL, VenueNames: []string{"Jurong East"},
		SessionStateBase64: base64.StdEncoding.EncodeToString([]byte(`{"cookies":[],"origins":[]}`)),
	}
	cfg.Sources[activesg.SourceID] = activeSG
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	adapter, err := service.sources.Adapter(activesg.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*fallback.Adapter); !ok {
		t.Fatalf("myactivesg adapter = %T, want fallback chain", adapter)
	}
}

func TestOpenSelectsDedicatedPerfectGymReader(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	kallang := cfg.Sources[perfectgym.SourceID]
	kallang.Enabled = true
	kallang.PerfectGym = config.SourcePerfectGym{
		Enabled: true, AvailabilityURL: "https://thekallang.perfectgym.com/clientportal2/",
		SessionStateBase64: base64.StdEncoding.EncodeToString([]byte(`{"cookies":[],"origins":[]}`)),
	}
	cfg.Sources[perfectgym.SourceID] = kallang
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	adapter, err := service.sources.Adapter(perfectgym.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*perfectgym.Adapter); !ok {
		t.Fatalf("the-kallang adapter = %T, want dedicated PerfectGym reader", adapter)
	}
}

func TestOpenChainsGenericAndDedicatedPerfectGymReaders(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	kallang := cfg.Sources[perfectgym.SourceID]
	kallang.Enabled = true
	kallang.Public = config.SourcePublic{Enabled: true, AvailabilityURL: "https://thekallang.perfectgym.com/availability", SlotJSONSelector: "script#kaypoh-slots"}
	kallang.PerfectGym = config.SourcePerfectGym{
		Enabled: true, AvailabilityURL: "https://thekallang.perfectgym.com/clientportal2/",
		SessionStateBase64: base64.StdEncoding.EncodeToString([]byte(`{"cookies":[],"origins":[]}`)),
	}
	cfg.Sources[perfectgym.SourceID] = kallang
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	adapter, err := service.sources.Adapter(perfectgym.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*fallback.Adapter); !ok {
		t.Fatalf("the-kallang adapter = %T, want fallback chain", adapter)
	}
}

func TestOpenSelectsDedicatedOnePAReader(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	onePA := cfg.Sources[onepa.SourceID]
	onePA.Enabled = true
	onePA.OnePA = config.SourceOnePA{Enabled: true, FacilityIDs: []string{"WoodlandsCC_BADMINTONCOURTS"}}
	cfg.Sources[onepa.SourceID] = onePA
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	adapter, err := service.sources.Adapter(onepa.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*onepa.Adapter); !ok {
		t.Fatalf("onepa adapter = %T, want dedicated onePA reader", adapter)
	}
}

func TestOpenChainsGenericAndDedicatedOnePAReaders(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	onePA := cfg.Sources[onepa.SourceID]
	onePA.Enabled = true
	onePA.Public = config.SourcePublic{Enabled: true, AvailabilityURL: "https://www.onepa.gov.sg/availability", SlotJSONSelector: "script#kaypoh-slots"}
	onePA.OnePA = config.SourceOnePA{Enabled: true, FacilityIDs: []string{"WoodlandsCC_BADMINTONCOURTS"}}
	cfg.Sources[onepa.SourceID] = onePA
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	adapter, err := service.sources.Adapter(onepa.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*fallback.Adapter); !ok {
		t.Fatalf("onepa adapter = %T, want fallback chain", adapter)
	}
}

func TestRouteFallsBackWithoutOptionalOneMapCredentials(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
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
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	if err := service.store.UpsertVenues(context.Background(), []domain.Venue{{
		ID: "venue", Name: "Venue", Coordinates: domain.Coordinates{Latitude: 1.3, Longitude: 103.8},
		Provenance: domain.Provenance{SourceID: "local-manual", FetchedAt: now},
	}}); err != nil {
		t.Fatal(err)
	}
	price := int64(1600)
	if _, err := service.ImportManualAvailability(context.Background(), []domain.AvailabilitySlot{{
		VenueID: "venue", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), PriceCents: &price,
	}}); err != nil {
		t.Fatal(err)
	}
	results, err := service.Search(context.Background(), domain.Query{MinimumDuration: time.Hour, Ranking: domain.RankCheap})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Slot.SourceID != "local-manual" || results[0].Breakdown.CourtPriceCents == nil {
		t.Fatalf("Search() = %#v", results)
	}
}

func TestRefreshHonoursSourcePollFloor(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	now := time.Now().UTC()
	if err := service.store.SaveSourceHealth(context.Background(), domain.SourceHealth{SourceID: "sportsg-facilities", State: domain.HealthHealthy, LastSuccess: &now}); err != nil {
		t.Fatal(err)
	}
	results, err := service.Refresh(context.Background(), []string{"sportsg-facilities"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].State != "skipped" || !strings.Contains(results[0].Detail, "poll floor") {
		t.Fatalf("Refresh() = %#v", results)
	}
}
