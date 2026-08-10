// Package ranking produces inspectable, commute-aware ordering for candidates.
package ranking

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/geo"
	"github.com/gongahkia/kaypoh/internal/query"
)

type scored struct {
	result        domain.SearchResult
	priceKnown    bool
	priceCents    int64
	travelSeconds int64
}

// Rank returns only candidates that satisfy known participant time constraints.
// A routing failure degrades that candidate's explanation and score rather than
// making unrelated local availability disappear.
func Rank(ctx context.Context, candidates []query.Candidate, input domain.Query, router geo.Router, now time.Time) ([]domain.SearchResult, error) {
	criteria, err := query.Normalize(input)
	if err != nil {
		return nil, err
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		item, fits, err := scoreCandidate(ctx, candidate, criteria, router, now)
		if err != nil {
			return nil, err
		}
		if fits {
			scoredCandidates = append(scoredCandidates, item)
		}
	}
	maxPrice, maxTravel := maxima(scoredCandidates)
	for index := range scoredCandidates {
		breakdown := &scoredCandidates[index].result.Breakdown
		priceScore := 0.35
		if scoredCandidates[index].priceKnown {
			priceScore = inverseRatio(float64(scoredCandidates[index].priceCents), float64(maxPrice))
		}
		travelScore := 1.0
		if maxTravel > 0 {
			travelScore = inverseRatio(float64(scoredCandidates[index].travelSeconds), float64(maxTravel))
		}
		fairnessScore := 1.0 - breakdown.FairnessPenalty
		weights := presetWeights(criteria.Ranking)
		breakdown.FinalScore = 100 * (weights.price*priceScore + weights.travel*travelScore + weights.fairness*fairnessScore + weights.freshness*breakdown.Freshness)
		breakdown.Reasons = append(breakdown.Reasons, fmt.Sprintf("score: %s prioritizes price %.0f%%, travel %.0f%%, fairness %.0f%%", criteria.Ranking, weights.price*100, weights.travel*100, weights.fairness*100))
	}
	sort.Slice(scoredCandidates, func(i, j int) bool {
		left, right := scoredCandidates[i].result, scoredCandidates[j].result
		if left.Breakdown.FinalScore != right.Breakdown.FinalScore {
			return left.Breakdown.FinalScore > right.Breakdown.FinalScore
		}
		if !left.Slot.Start.Equal(right.Slot.Start) {
			return left.Slot.Start.Before(right.Slot.Start)
		}
		return left.Venue.ID < right.Venue.ID
	})
	results := make([]domain.SearchResult, len(scoredCandidates))
	for index, candidate := range scoredCandidates {
		results[index] = candidate.result
	}
	return results, nil
}

func scoreCandidate(ctx context.Context, candidate query.Candidate, criteria domain.Query, router geo.Router, now time.Time) (scored, bool, error) {
	result := domain.SearchResult{Slot: candidate.Slot, Venue: candidate.Venue, ComponentSlotIDs: append([]string(nil), candidate.ComponentSlotIDs...)}
	breakdown := &result.Breakdown
	breakdown.SlotFit = 1
	breakdown.Freshness = freshness(candidate.Slot, now)
	if candidate.Slot.PriceCents != nil {
		price := *candidate.Slot.PriceCents
		breakdown.CourtPriceCents = &price
		if criteria.Participants > 0 {
			perPerson := (price + int64(criteria.Participants) - 1) / int64(criteria.Participants)
			breakdown.PricePerPersonCents = &perPerson
			breakdown.Reasons = append(breakdown.Reasons, fmt.Sprintf("price: %s%d total, %s%d per person", currencyPrefix(candidate.Slot.Currency), price, currencyPrefix(candidate.Slot.Currency), perPerson))
		} else {
			breakdown.Reasons = append(breakdown.Reasons, fmt.Sprintf("price: %s%d total", currencyPrefix(candidate.Slot.Currency), price))
		}
	} else {
		breakdown.Reasons = append(breakdown.Reasons, "price: not published by this availability record")
	}
	if !candidate.Slot.StaleAfter.IsZero() {
		breakdown.Reasons = append(breakdown.Reasons, fmt.Sprintf("freshness: %.0f%% of 24-hour window remains", breakdown.Freshness*100))
	} else {
		breakdown.Reasons = append(breakdown.Reasons, "freshness: source did not declare an expiry")
	}
	if criteria.Commute == nil || len(criteria.Commute.Participants) == 0 {
		return scored{result: result, priceKnown: candidate.Slot.PriceCents != nil, priceCents: priceValue(candidate.Slot.PriceCents)}, true, nil
	}
	if !hasCoordinates(candidate.Venue.Coordinates) {
		breakdown.FairnessPenalty = 1
		breakdown.Reasons = append(breakdown.Reasons, "commute: venue coordinates unavailable")
		return scored{result: result, priceKnown: candidate.Slot.PriceCents != nil, priceCents: priceValue(candidate.Slot.PriceCents)}, true, nil
	}
	participantTravel := make([]int64, 0, len(criteria.Commute.Participants))
	for _, participant := range criteria.Commute.Participants {
		mode := geo.TravelMode(participant.Mode)
		if mode == "" {
			mode = geo.ModePublicTransport
		}
		originRoute, routeErr := route(ctx, router, participant.Origin, candidate.Venue.Coordinates, mode)
		if routeErr != nil {
			breakdown.FallbackRouting = true
			breakdown.Reasons = append(breakdown.Reasons, "commute: route unavailable for "+participantLabel(participant))
			continue
		}
		originSeconds := int64(math.Round(originRoute.Duration.Seconds()))
		breakdown.OriginCommuteSeconds = append(breakdown.OriginCommuteSeconds, originSeconds)
		breakdown.TotalTravelSeconds += originSeconds
		participantSeconds := originSeconds
		breakdown.FallbackRouting = breakdown.FallbackRouting || originRoute.Fallback
		if participant.EarliestDeparture != nil && candidate.Slot.Start.Add(-originRoute.Duration).Before(*participant.EarliestDeparture) {
			return scored{}, false, nil
		}
		if participant.Destination != nil {
			homeRoute, routeErr := route(ctx, router, candidate.Venue.Coordinates, *participant.Destination, mode)
			if routeErr != nil {
				breakdown.FallbackRouting = true
				breakdown.Reasons = append(breakdown.Reasons, "return commute: route unavailable for "+participantLabel(participant))
			} else {
				homeSeconds := int64(math.Round(homeRoute.Duration.Seconds()))
				breakdown.HomeCommuteSeconds = append(breakdown.HomeCommuteSeconds, homeSeconds)
				breakdown.TotalTravelSeconds += homeSeconds
				participantSeconds += homeSeconds
				breakdown.FallbackRouting = breakdown.FallbackRouting || homeRoute.Fallback
				if participant.LatestArrival != nil && candidate.Slot.End.Add(homeRoute.Duration).After(*participant.LatestArrival) {
					return scored{}, false, nil
				}
			}
		}
		participantTravel = append(participantTravel, participantSeconds)
		breakdown.MaximumTravelSeconds = max(breakdown.MaximumTravelSeconds, participantSeconds)
	}
	if len(participantTravel) > 0 {
		average := float64(breakdown.TotalTravelSeconds) / float64(len(participantTravel))
		if breakdown.MaximumTravelSeconds > 0 {
			breakdown.FairnessPenalty = clamp((float64(breakdown.MaximumTravelSeconds)-average)/float64(breakdown.MaximumTravelSeconds), 0, 1)
		}
		breakdown.Reasons = append(breakdown.Reasons, fmt.Sprintf("commute: %d minutes total; longest participant %d minutes", breakdown.TotalTravelSeconds/60, breakdown.MaximumTravelSeconds/60))
	}
	return scored{result: result, priceKnown: candidate.Slot.PriceCents != nil, priceCents: priceValue(candidate.Slot.PriceCents), travelSeconds: breakdown.TotalTravelSeconds}, true, nil
}

func route(ctx context.Context, router geo.Router, origin, destination domain.Coordinates, mode geo.TravelMode) (geo.Route, error) {
	if router == nil {
		return geo.FallbackRoute(origin, destination, mode), nil
	}
	return router.Route(ctx, origin, destination, mode)
}

type weights struct {
	price, travel, fairness, freshness float64
}

func presetWeights(preset domain.RankingPreset) weights {
	switch preset {
	case domain.RankCheap:
		return weights{price: .65, travel: .15, fairness: .10, freshness: .10}
	case domain.RankCommute:
		return weights{price: .15, travel: .55, fairness: .20, freshness: .10}
	case domain.RankFair:
		return weights{price: .15, travel: .25, fairness: .50, freshness: .10}
	default:
		return weights{price: .40, travel: .30, fairness: .20, freshness: .10}
	}
}

func maxima(candidates []scored) (int64, int64) {
	var price, travel int64
	for _, candidate := range candidates {
		if candidate.priceKnown {
			price = max(price, candidate.priceCents)
		}
		travel = max(travel, candidate.travelSeconds)
	}
	return price, travel
}

func freshness(slot domain.AvailabilitySlot, now time.Time) float64 {
	if slot.StaleAfter.IsZero() {
		return 1
	}
	return clamp(slot.StaleAfter.Sub(now).Hours()/24, 0, 1)
}

func inverseRatio(value, maximum float64) float64 {
	if maximum <= 0 {
		return 1
	}
	return clamp(1-value/maximum, 0, 1)
}

func priceValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func currencyPrefix(currency string) string {
	if currency == "" || currency == "SGD" {
		return "S$"
	}
	return currency + " "
}

func participantLabel(participant domain.Participant) string {
	if participant.Name != "" {
		return participant.Name
	}
	return "participant"
}

func hasCoordinates(value domain.Coordinates) bool {
	return value.Valid() && (value.Latitude != 0 || value.Longitude != 0)
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func max(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
