package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
