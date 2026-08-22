// Package query filters normalized availability without making upstream claims.
package query

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/geo"
	"github.com/gongahkia/kaypoh/internal/store"
)

// Candidate is a bookable normalized interval. ComponentSlotIDs makes a
// contiguous result auditable when multiple upstream slots form one interval.
type Candidate struct {
	Slot             domain.AvailabilitySlot `json:"slot"`
	Venue            domain.Venue            `json:"venue"`
	ComponentSlotIDs []string                `json:"component_slot_ids,omitempty"`
}

// Normalize validates a badminton-only query.
func Normalize(input domain.Query) (domain.Query, error) {
	query := input
	if query.MinimumDuration < 0 {
		return domain.Query{}, fmt.Errorf("minimum duration cannot be negative")
	}
	if query.MaximumPriceCents != nil && *query.MaximumPriceCents < 0 {
		return domain.Query{}, fmt.Errorf("maximum price cannot be negative")
	}
	if query.MaximumPricePerPersonCents != nil {
		if *query.MaximumPricePerPersonCents < 0 {
			return domain.Query{}, fmt.Errorf("maximum price per person cannot be negative")
		}
		if query.Participants <= 0 {
			return domain.Query{}, fmt.Errorf("participants must be positive when filtering price per person")
		}
	}
	if query.Participants < 0 {
		return domain.Query{}, fmt.Errorf("participants cannot be negative")
	}
	if query.Commute != nil {
		for index, participant := range query.Commute.Participants {
			if !participant.Origin.Valid() || !hasCoordinates(participant.Origin) {
				return domain.Query{}, fmt.Errorf("commute participant %d needs valid origin coordinates", index+1)
			}
			if participant.Destination != nil && (!participant.Destination.Valid() || !hasCoordinates(*participant.Destination)) {
				return domain.Query{}, fmt.Errorf("commute participant %d has invalid destination coordinates", index+1)
			}
			if participant.Mode != "" && !geo.TravelMode(participant.Mode).Valid() {
				return domain.Query{}, fmt.Errorf("commute participant %d has invalid travel mode %q", index+1, participant.Mode)
			}
		}
		if query.Participants == 0 && len(query.Commute.Participants) > 0 {
			query.Participants = len(query.Commute.Participants)
		}
	}
	if query.StartDate != nil && query.EndDate != nil && localDate(*query.EndDate).Before(localDate(*query.StartDate)) {
		return domain.Query{}, fmt.Errorf("end date cannot be before start date")
	}
	if query.EarliestStartMinute != nil && (*query.EarliestStartMinute < 0 || *query.EarliestStartMinute >= 24*60) {
		return domain.Query{}, fmt.Errorf("earliest start minute must be between 0 and 1439")
	}
	if query.LatestEndMinute != nil && (*query.LatestEndMinute < 0 || *query.LatestEndMinute > 24*60) {
		return domain.Query{}, fmt.Errorf("latest end minute must be between 0 and 1440")
	}
	if query.LocationRadius != nil {
		if !query.LocationRadius.Center.Valid() || !hasCoordinates(query.LocationRadius.Center) || query.LocationRadius.RadiusMeters <= 0 {
			return domain.Query{}, fmt.Errorf("location radius needs valid coordinates and a positive radius")
		}
	}
	switch query.Ranking {
	case "", domain.RankBalanced:
		query.Ranking = domain.RankBalanced
	case domain.RankCheap, domain.RankCommute, domain.RankFair:
	default:
		return domain.Query{}, fmt.Errorf("unknown ranking preset %q", query.Ranking)
	}
	query.Sources = normalizedStrings(query.Sources)
	query.VenueIDs = normalizedStrings(query.VenueIDs)
	return query, nil
}

// Filter returns fresh, available candidates that satisfy query constraints.
// It does not derive availability from venue metadata or from static price data.
func Filter(rows []store.SlotWithVenue, input domain.Query, now time.Time) ([]Candidate, error) {
	query, err := Normalize(input)
	if err != nil {
		return nil, err
	}
	base := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		candidate := Candidate{Slot: row.Slot, Venue: row.Venue, ComponentSlotIDs: []string{row.Slot.ID}}
		if matchesStatic(candidate, query, now) {
			base = append(base, candidate)
		}
	}
	if query.MinimumDuration > 0 {
		base = composeContiguous(base, query.MinimumDuration)
	}
	results := make([]Candidate, 0, len(base))
	for _, candidate := range base {
		if matchesFinal(candidate, query) {
			results = append(results, candidate)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if !results[i].Slot.Start.Equal(results[j].Slot.Start) {
			return results[i].Slot.Start.Before(results[j].Slot.Start)
		}
		if results[i].Venue.Name != results[j].Venue.Name {
			return results[i].Venue.Name < results[j].Venue.Name
		}
		return results[i].Slot.ID < results[j].Slot.ID
	})
	return results, nil
}

func matchesStatic(candidate Candidate, query domain.Query, now time.Time) bool {
	slot := candidate.Slot
	if slot.Status != domain.AvailabilityAvailable || !slot.Fresh(now) {
		return false
	}
	if len(query.Sources) > 0 && !contains(query.Sources, slot.SourceID) {
		return false
	}
	if len(query.VenueIDs) > 0 && !contains(query.VenueIDs, slot.VenueID) {
		return false
	}
	if query.StartDate != nil && localDate(slot.Start).Before(localDate(*query.StartDate)) {
		return false
	}
	if query.EndDate != nil && localDate(slot.Start).After(localDate(*query.EndDate)) {
		return false
	}
	if len(query.Days) > 0 && !containsWeekday(query.Days, slot.Start.In(singaporeLocation()).Weekday()) {
		return false
	}
	if query.EarliestStartMinute != nil && minuteOfDay(slot.Start) < *query.EarliestStartMinute {
		return false
	}
	if query.LatestEndMinute != nil && minuteOfDay(slot.End) > *query.LatestEndMinute {
		return false
	}
	if query.Indoor != nil && (candidate.Venue.Indoor == nil || *candidate.Venue.Indoor != *query.Indoor) {
		return false
	}
	if query.Sheltered != nil && (candidate.Venue.Sheltered == nil || *candidate.Venue.Sheltered != *query.Sheltered) {
		return false
	}
	if query.MembershipRequired != nil && (slot.MembershipRequired == nil || *slot.MembershipRequired != *query.MembershipRequired) {
		return false
	}
	if query.LocationRadius != nil {
		if !hasCoordinates(candidate.Venue.Coordinates) || geo.HaversineMeters(query.LocationRadius.Center, candidate.Venue.Coordinates) > query.LocationRadius.RadiusMeters {
			return false
		}
	}
	return true
}

func matchesFinal(candidate Candidate, query domain.Query) bool {
	if query.MinimumDuration > 0 && candidate.Slot.End.Sub(candidate.Slot.Start) < query.MinimumDuration {
		return false
	}
	if query.MaximumPriceCents != nil && (candidate.Slot.PriceCents == nil || *candidate.Slot.PriceCents > *query.MaximumPriceCents) {
		return false
	}
	if query.MaximumPricePerPersonCents != nil {
		if candidate.Slot.PriceCents == nil {
			return false
		}
		perPerson := (*candidate.Slot.PriceCents + int64(query.Participants) - 1) / int64(query.Participants)
		if perPerson > *query.MaximumPricePerPersonCents {
			return false
		}
	}
	return true
}

func composeContiguous(input []Candidate, minimum time.Duration) []Candidate {
	groups := make(map[string][]Candidate)
	for _, candidate := range input {
		key := strings.Join([]string{candidate.Slot.SourceID, candidate.Slot.VenueID, candidate.Slot.FacilityID, candidate.Slot.CourtName}, "\x00")
		groups[key] = append(groups[key], candidate)
	}
	result := []Candidate{}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			if !group[i].Slot.Start.Equal(group[j].Slot.Start) {
				return group[i].Slot.Start.Before(group[j].Slot.Start)
			}
			return group[i].Slot.ID < group[j].Slot.ID
		})
		current := group[0]
		for _, next := range group[1:] {
			if current.Slot.End.Equal(next.Slot.Start) && compatible(current.Slot, next.Slot) {
				current = combine(current, next)
				continue
			}
			if current.Slot.End.Sub(current.Slot.Start) >= minimum {
				result = append(result, current)
			}
			current = next
		}
		if current.Slot.End.Sub(current.Slot.Start) >= minimum {
			result = append(result, current)
		}
	}
	return result
}

func compatible(first, second domain.AvailabilitySlot) bool {
	if first.Status != second.Status || !sameOptionalBool(first.MembershipRequired, second.MembershipRequired) {
		return false
	}
	return first.PriceCents == nil || second.PriceCents == nil || first.Currency == second.Currency
}

func combine(first, second Candidate) Candidate {
	combined := first
	combined.Slot.End = second.Slot.End
	combined.Slot.ObservedAt = earliestNonZero(first.Slot.ObservedAt, second.Slot.ObservedAt)
	combined.Slot.FetchedAt = earliestNonZero(first.Slot.FetchedAt, second.Slot.FetchedAt)
	combined.Slot.StaleAfter = earliestNonZero(first.Slot.StaleAfter, second.Slot.StaleAfter)
	if first.Slot.PriceCents == nil || second.Slot.PriceCents == nil {
		combined.Slot.PriceCents = nil
		if first.Slot.PriceCents == nil {
			combined.Slot.Currency = second.Slot.Currency
		}
	} else {
		amount := *first.Slot.PriceCents + *second.Slot.PriceCents
		combined.Slot.PriceCents = &amount
	}
	if first.Slot.BookingURL != second.Slot.BookingURL {
		combined.Slot.BookingURL = ""
	}
	combined.ComponentSlotIDs = append(append([]string(nil), first.ComponentSlotIDs...), second.ComponentSlotIDs...)
	digest := sha256.Sum256([]byte(strings.Join(combined.ComponentSlotIDs, "\x00")))
	combined.Slot.ID = "composite:" + fmt.Sprintf("%x", digest[:8])
	return combined
}

func earliestNonZero(first, second time.Time) time.Time {
	if first.IsZero() || (!second.IsZero() && second.Before(first)) {
		return second
	}
	return first
}

func normalizedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func containsWeekday(values []time.Weekday, needle time.Weekday) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func singaporeLocation() *time.Location {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return location
}

func localDate(value time.Time) time.Time {
	local := value.In(singaporeLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, singaporeLocation())
}

func minuteOfDay(value time.Time) int {
	local := value.In(singaporeLocation())
	return local.Hour()*60 + local.Minute()
}

func hasCoordinates(value domain.Coordinates) bool {
	return value.Valid() && (value.Latitude != 0 || value.Longitude != 0)
}

func sameOptionalBool(first, second *bool) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}
