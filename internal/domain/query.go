package domain

import (
	"time"
)

type RankingPreset string

const (
	RankCheap    RankingPreset = "cheap"
	RankBalanced RankingPreset = "balanced"
	RankCommute  RankingPreset = "commute"
	RankFair     RankingPreset = "fair"
)

type LocationRadius struct {
	Center       Coordinates `json:"center"`
	RadiusMeters int         `json:"radius_meters"`
}

type Participant struct {
	Name              string       `json:"name,omitempty"`
	Origin            Coordinates  `json:"origin"`
	Destination       *Coordinates `json:"destination,omitempty"`
	Mode              string       `json:"mode"`
	EarliestDeparture *time.Time   `json:"earliest_departure,omitempty"`
	LatestArrival     *time.Time   `json:"latest_arrival,omitempty"`
}

type CommuteProfile struct {
	Participants []Participant `json:"participants"`
}

type Query struct {
	Sources                    []string        `json:"sources,omitempty"`
	VenueIDs                   []string        `json:"venue_ids,omitempty"`
	StartDate                  *time.Time      `json:"start_date,omitempty"`
	EndDate                    *time.Time      `json:"end_date,omitempty"`
	Days                       []time.Weekday  `json:"days,omitempty"`
	EarliestStartMinute        *int            `json:"earliest_start_minute,omitempty"`
	LatestEndMinute            *int            `json:"latest_end_minute,omitempty"`
	MinimumDuration            time.Duration   `json:"minimum_duration"`
	MaximumPriceCents          *int64          `json:"maximum_price_cents,omitempty"`
	Participants               int             `json:"participants,omitempty"`
	MaximumPricePerPersonCents *int64          `json:"maximum_price_per_person_cents,omitempty"`
	Indoor                     *bool           `json:"indoor,omitempty"`
	Sheltered                  *bool           `json:"sheltered,omitempty"`
	MembershipRequired         *bool           `json:"membership_required,omitempty"`
	LocationRadius             *LocationRadius `json:"location_radius,omitempty"`
	Commute                    *CommuteProfile `json:"commute,omitempty"`
	Ranking                    RankingPreset   `json:"ranking,omitempty"`
}

type RankingBreakdown struct {
	SlotFit              float64  `json:"slot_fit"`
	CourtPriceCents      *int64   `json:"court_price_cents,omitempty"`
	PricePerPersonCents  *int64   `json:"price_per_person_cents,omitempty"`
	OriginCommuteSeconds []int64  `json:"origin_commute_seconds,omitempty"`
	HomeCommuteSeconds   []int64  `json:"home_commute_seconds,omitempty"`
	TotalTravelSeconds   int64    `json:"total_travel_seconds"`
	MaximumTravelSeconds int64    `json:"maximum_travel_seconds"`
	FairnessPenalty      float64  `json:"fairness_penalty"`
	Freshness            float64  `json:"freshness"`
	FallbackRouting      bool     `json:"fallback_routing"`
	FinalScore           float64  `json:"final_score"`
	Reasons              []string `json:"reasons"`
}

type SearchResult struct {
	Slot             AvailabilitySlot `json:"slot"`
	Venue            Venue            `json:"venue"`
	ComponentSlotIDs []string         `json:"component_slot_ids,omitempty"`
	Breakdown        RankingBreakdown `json:"breakdown"`
}
