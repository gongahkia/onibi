package domain

import (
	"time"
)

const SingaporeTimeZone = "Asia/Singapore"

// Coordinates uses WGS84 latitude and longitude.
type Coordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (c Coordinates) Valid() bool {
	return c.Latitude >= -90 && c.Latitude <= 90 && c.Longitude >= -180 && c.Longitude <= 180
}

type Provenance struct {
	SourceID        string    `json:"source_id"`
	SourceReference string    `json:"source_reference,omitempty"`
	ObservedAt      time.Time `json:"observed_at"`
	FetchedAt       time.Time `json:"fetched_at"`
	AdapterVersion  string    `json:"adapter_version,omitempty"`
	EvidenceHash    string    `json:"evidence_hash,omitempty"`
	Confidence      float64   `json:"confidence"`
}

type Venue struct {
	ID             string      `json:"id"`
	SourceIDs      []string    `json:"source_ids,omitempty"`
	Name           string      `json:"name"`
	Address        string      `json:"address,omitempty"`
	PostalCode     string      `json:"postal_code,omitempty"`
	Coordinates    Coordinates `json:"coordinates"`
	Classification string      `json:"classification,omitempty"`
	Sports         []string    `json:"sports,omitempty"`
	Amenities      []string    `json:"amenities,omitempty"`
	Indoor         *bool       `json:"indoor,omitempty"`
	Sheltered      *bool       `json:"sheltered,omitempty"`
	BookingURLs    []string    `json:"booking_urls,omitempty"`
	Provenance     Provenance  `json:"provenance"`
}

type Facility struct {
	ID               string     `json:"id"`
	VenueID          string     `json:"venue_id"`
	SourceID         string     `json:"source_id"`
	SourceFacilityID string     `json:"source_facility_id,omitempty"`
	Name             string     `json:"name"`
	CourtType        string     `json:"court_type,omitempty"`
	Sports           []string   `json:"sports,omitempty"`
	Attributes       []string   `json:"attributes,omitempty"`
	Provenance       Provenance `json:"provenance"`
}

type PriceRule struct {
	ID            string         `json:"id"`
	VenueID       string         `json:"venue_id"`
	FacilityID    string         `json:"facility_id,omitempty"`
	Currency      string         `json:"currency"`
	AmountCents   int64          `json:"amount_cents"`
	Unit          string         `json:"unit"`
	Peak          *bool          `json:"peak,omitempty"`
	Member        *bool          `json:"member,omitempty"`
	Resident      *bool          `json:"resident,omitempty"`
	Days          []time.Weekday `json:"days,omitempty"`
	StartMinute   *int           `json:"start_minute,omitempty"`
	EndMinute     *int           `json:"end_minute,omitempty"`
	EffectiveFrom *time.Time     `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time     `json:"effective_to,omitempty"`
	Provenance    Provenance     `json:"provenance"`
}

type AvailabilityStatus string

const (
	AvailabilityAvailable   AvailabilityStatus = "available"
	AvailabilityUnavailable AvailabilityStatus = "unavailable"
	AvailabilityUnknown     AvailabilityStatus = "unknown"
)

// AvailabilitySlot uses a half-open interval [Start, End) in Asia/Singapore.
type AvailabilitySlot struct {
	ID                 string             `json:"id"`
	SportID            string             `json:"sport_id"`
	VenueID            string             `json:"venue_id"`
	FacilityID         string             `json:"facility_id,omitempty"`
	SourceID           string             `json:"source_id"`
	Start              time.Time          `json:"start"`
	End                time.Time          `json:"end"`
	Status             AvailabilityStatus `json:"status"`
	PriceCents         *int64             `json:"price_cents,omitempty"`
	Currency           string             `json:"currency,omitempty"`
	MembershipRequired *bool              `json:"membership_required,omitempty"`
	BookingURL         string             `json:"booking_url,omitempty"`
	ObservedAt         time.Time          `json:"observed_at"`
	FetchedAt          time.Time          `json:"fetched_at"`
	StaleAfter         time.Time          `json:"stale_after"`
	Provenance         Provenance         `json:"provenance"`
}

func (slot AvailabilitySlot) Valid() bool {
	return slot.ID != "" && slot.SportID != "" && slot.VenueID != "" && slot.SourceID != "" &&
		slot.End.After(slot.Start) && slot.Status != ""
}

func (slot AvailabilitySlot) Fresh(now time.Time) bool {
	return slot.StaleAfter.IsZero() || now.Before(slot.StaleAfter)
}

type SourceObservation struct {
	ID           string    `json:"id"`
	SourceID     string    `json:"source_id"`
	Reference    string    `json:"reference"`
	FetchedAt    time.Time `json:"fetched_at"`
	Parser       string    `json:"parser"`
	EvidenceHash string    `json:"evidence_hash,omitempty"`
	Confidence   float64   `json:"confidence"`
}
