package ranking

import (
	"context"
	"testing"
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
	"github.com/gongahkia/courtsg/internal/geo"
	"github.com/gongahkia/courtsg/internal/query"
)

type fixedRouter struct{}

func (fixedRouter) Route(_ context.Context, origin, destination domain.Coordinates, mode geo.TravelMode) (geo.Route, error) {
	duration := 10 * time.Minute
	if destination.Latitude > origin.Latitude {
		duration = 20 * time.Minute
	}
	return geo.Route{Origin: origin, Destination: destination, Mode: mode, Duration: duration, DistanceMeters: 4000, Provider: "test"}, nil
}

func TestRankAppliesParticipantTimeConstraintAndExposesBreakdown(t *testing.T) {
	now := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	price := int64(1200)
	candidates := []query.Candidate{
		{Slot: domain.AvailabilitySlot{ID: "fits", Start: now.Add(2 * time.Hour), End: now.Add(3 * time.Hour), PriceCents: &price, Currency: "SGD", StaleAfter: now.Add(12 * time.Hour)}, Venue: domain.Venue{ID: "one", Name: "one", Coordinates: domain.Coordinates{Latitude: 1.31, Longitude: 103.8}}, ComponentSlotIDs: []string{"fits"}},
		{Slot: domain.AvailabilitySlot{ID: "late", Start: now.Add(30 * time.Minute), End: now.Add(90 * time.Minute), PriceCents: &price, Currency: "SGD", StaleAfter: now.Add(12 * time.Hour)}, Venue: domain.Venue{ID: "two", Name: "two", Coordinates: domain.Coordinates{Latitude: 1.31, Longitude: 103.8}}, ComponentSlotIDs: []string{"late"}},
	}
	earliest := now.Add(time.Hour)
	results, err := Rank(context.Background(), candidates, domain.Query{Participants: 2, Ranking: domain.RankCommute, Commute: &domain.CommuteProfile{Participants: []domain.Participant{{Name: "A", Origin: domain.Coordinates{Latitude: 1.30, Longitude: 103.8}, EarliestDeparture: &earliest, Mode: "pt"}}}}, fixedRouter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Slot.ID != "fits" {
		t.Fatalf("Rank() = %#v", results)
	}
	if results[0].Breakdown.FinalScore <= 0 || len(results[0].Breakdown.Reasons) < 3 || results[0].Breakdown.PricePerPersonCents == nil || *results[0].Breakdown.PricePerPersonCents != 600 {
		t.Fatalf("breakdown = %#v", results[0].Breakdown)
	}
}
