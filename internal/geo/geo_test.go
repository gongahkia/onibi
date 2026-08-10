package geo

import (
	"testing"

	"github.com/gongahkia/kaypoh/internal/domain"
)

func TestHaversineAndFallbackAreDeterministic(t *testing.T) {
	origin := domain.Coordinates{Latitude: 1.300, Longitude: 103.800}
	destination := domain.Coordinates{Latitude: 1.310, Longitude: 103.810}
	distance := HaversineMeters(origin, destination)
	if distance < 1_500 || distance > 1_600 {
		t.Fatalf("distance = %d", distance)
	}
	route := FallbackRoute(origin, destination, ModeWalk)
	if !route.Fallback || route.DistanceMeters != distance || route.Duration <= 0 {
		t.Fatalf("unexpected fallback route: %#v", route)
	}
}
