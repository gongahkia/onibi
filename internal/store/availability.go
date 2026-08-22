package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

type SlotWithVenue struct {
	Slot  domain.AvailabilitySlot
	Venue domain.Venue
}

func (store *Store) UpsertAvailability(ctx context.Context, slots []domain.AvailabilitySlot) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin availability transaction: %w", err)
	}
	defer tx.Rollback()
	for _, slot := range slots {
		if !slot.Valid() {
			return fmt.Errorf("invalid availability slot %q", slot.ID)
		}
		provenance, err := json.Marshal(slot.Provenance)
		if err != nil {
			return fmt.Errorf("encode availability slot %q provenance: %w", slot.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO availability_slots(id, venue_id, facility_id, court_name, source_id, start_at, end_at, status, price_cents, currency, membership_required, booking_url, observed_at, fetched_at, stale_after, provenance_json, created_at, updated_at)
VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET start_at = excluded.start_at, end_at = excluded.end_at, status = excluded.status,
price_cents = excluded.price_cents, currency = excluded.currency, court_name = excluded.court_name, booking_url = excluded.booking_url,
membership_required = excluded.membership_required,
observed_at = excluded.observed_at, fetched_at = excluded.fetched_at, stale_after = excluded.stale_after,
provenance_json = excluded.provenance_json, updated_at = excluded.updated_at`,
			slot.ID, slot.VenueID, slot.FacilityID, slot.CourtName, slot.SourceID, timestamp(slot.Start), timestamp(slot.End), slot.Status,
			nullableInt64(slot.PriceCents), slot.Currency, nullableBool(slot.MembershipRequired), slot.BookingURL, timestamp(slot.ObservedAt), timestamp(slot.FetchedAt), timestamp(slot.StaleAfter), string(provenance), timestamp(time.Now()), timestamp(time.Now())); err != nil {
			return fmt.Errorf("upsert availability slot %q: %w", slot.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit availability transaction: %w", err)
	}
	return nil
}

func (store *Store) ListAvailability(ctx context.Context, from, until time.Time, limit int) ([]SlotWithVenue, error) {
	if limit <= 0 || limit > 2_000 {
		limit = 500
	}
	rows, err := store.db.QueryContext(ctx, `SELECT a.id, a.venue_id, COALESCE(a.facility_id, ''), a.court_name, a.source_id,
a.start_at, a.end_at, a.status, a.price_cents, a.currency, a.membership_required, a.booking_url, a.observed_at, a.fetched_at, a.stale_after, a.provenance_json,
v.id, v.name, v.address, v.postal_code, v.latitude, v.longitude, v.classification, v.sports_json, v.amenities_json,
v.indoor, v.sheltered, v.booking_urls_json, v.provenance_json
FROM availability_slots a JOIN venues v ON v.id = a.venue_id
WHERE a.end_at > ? AND a.start_at < ? ORDER BY a.start_at, a.id LIMIT ?`, timestamp(from), timestamp(until), limit)
	if err != nil {
		return nil, fmt.Errorf("list availability: %w", err)
	}
	defer rows.Close()
	results := []SlotWithVenue{}
	for rows.Next() {
		result, err := scanSlotWithVenue(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate availability: %w", err)
	}
	return results, nil
}

type slotRowScanner interface {
	Scan(...any) error
}

func scanSlotWithVenue(row slotRowScanner) (SlotWithVenue, error) {
	var result SlotWithVenue
	var start, end, observedAt, fetchedAt, staleAfter string
	var price, membershipRequired sql.NullInt64
	var slotProvenance, ignoredSports, amenities, bookingURLs, venueProvenance string
	var latitude, longitude sql.NullFloat64
	var indoor, sheltered sql.NullInt64
	if err := row.Scan(&result.Slot.ID, &result.Slot.VenueID, &result.Slot.FacilityID, &result.Slot.CourtName, &result.Slot.SourceID,
		&start, &end, &result.Slot.Status, &price, &result.Slot.Currency, &membershipRequired, &result.Slot.BookingURL, &observedAt, &fetchedAt, &staleAfter, &slotProvenance,
		&result.Venue.ID, &result.Venue.Name, &result.Venue.Address, &result.Venue.PostalCode, &latitude, &longitude, &result.Venue.Classification, &ignoredSports, &amenities, &indoor, &sheltered, &bookingURLs, &venueProvenance); err != nil {
		return SlotWithVenue{}, fmt.Errorf("scan availability: %w", err)
	}
	var err error
	if result.Slot.Start, err = time.Parse(time.RFC3339Nano, start); err != nil {
		return SlotWithVenue{}, fmt.Errorf("parse availability %q start: %w", result.Slot.ID, err)
	}
	if result.Slot.End, err = time.Parse(time.RFC3339Nano, end); err != nil {
		return SlotWithVenue{}, fmt.Errorf("parse availability %q end: %w", result.Slot.ID, err)
	}
	if result.Slot.ObservedAt, err = time.Parse(time.RFC3339Nano, observedAt); err != nil {
		return SlotWithVenue{}, fmt.Errorf("parse availability %q observed time: %w", result.Slot.ID, err)
	}
	if result.Slot.FetchedAt, err = time.Parse(time.RFC3339Nano, fetchedAt); err != nil {
		return SlotWithVenue{}, fmt.Errorf("parse availability %q fetched time: %w", result.Slot.ID, err)
	}
	if result.Slot.StaleAfter, err = time.Parse(time.RFC3339Nano, staleAfter); err != nil {
		return SlotWithVenue{}, fmt.Errorf("parse availability %q stale time: %w", result.Slot.ID, err)
	}
	if price.Valid {
		result.Slot.PriceCents = &price.Int64
	}
	result.Slot.MembershipRequired = boolPointer(membershipRequired)
	if err := json.Unmarshal([]byte(slotProvenance), &result.Slot.Provenance); err != nil {
		return SlotWithVenue{}, fmt.Errorf("decode availability %q provenance: %w", result.Slot.ID, err)
	}
	if latitude.Valid && longitude.Valid {
		result.Venue.Coordinates = domain.Coordinates{Latitude: latitude.Float64, Longitude: longitude.Float64}
	}
	if err := json.Unmarshal([]byte(amenities), &result.Venue.Amenities); err != nil {
		return SlotWithVenue{}, fmt.Errorf("decode venue %q amenities: %w", result.Venue.ID, err)
	}
	if err := json.Unmarshal([]byte(bookingURLs), &result.Venue.BookingURLs); err != nil {
		return SlotWithVenue{}, fmt.Errorf("decode venue %q booking URLs: %w", result.Venue.ID, err)
	}
	if err := json.Unmarshal([]byte(venueProvenance), &result.Venue.Provenance); err != nil {
		return SlotWithVenue{}, fmt.Errorf("decode venue %q provenance: %w", result.Venue.ID, err)
	}
	result.Venue.Indoor = boolPointer(indoor)
	result.Venue.Sheltered = boolPointer(sheltered)
	return result, nil
}

// ReconcileAvailability marks an available slot unavailable only when a
// successful source snapshot covering its date range no longer contains it.
func (store *Store) ReconcileAvailability(ctx context.Context, sourceID string, from, until time.Time, observed []domain.AvailabilitySlot, now time.Time) error {
	seen := make(map[string]struct{}, len(observed))
	for _, slot := range observed {
		seen[slot.ID] = struct{}{}
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin availability reconciliation: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM availability_slots WHERE source_id = ? AND end_at > ? AND start_at < ? AND status = ?`, sourceID, timestamp(from), timestamp(until), domain.AvailabilityAvailable)
	if err != nil {
		return fmt.Errorf("list availability reconciliation candidates: %w", err)
	}
	defer rows.Close()
	missing := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan availability reconciliation candidate: %w", err)
		}
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate availability reconciliation candidates: %w", err)
	}
	for _, id := range missing {
		if _, err := tx.ExecContext(ctx, `UPDATE availability_slots SET status = ?, observed_at = ?, fetched_at = ?, stale_after = ?, updated_at = ? WHERE id = ?`, domain.AvailabilityUnavailable, timestamp(now), timestamp(now), timestamp(now), timestamp(now), id); err != nil {
			return fmt.Errorf("mark absent availability slot %q unavailable: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit availability reconciliation: %w", err)
	}
	return nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
