package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

type VenueFilter struct {
	Search string
	Limit  int
}

func (store *Store) UpsertVenues(ctx context.Context, venues []domain.Venue) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin venues transaction: %w", err)
	}
	defer tx.Rollback()
	for _, venue := range venues {
		if venue.ID == "" || venue.Name == "" || venue.Provenance.SourceID == "" {
			return fmt.Errorf("venue id, name, and provenance source are required")
		}
		amenities, err := json.Marshal(venue.Amenities)
		if err != nil {
			return fmt.Errorf("encode venue %q amenities: %w", venue.ID, err)
		}
		bookingURLs, err := json.Marshal(venue.BookingURLs)
		if err != nil {
			return fmt.Errorf("encode venue %q booking URLs: %w", venue.ID, err)
		}
		provenance, err := json.Marshal(venue.Provenance)
		if err != nil {
			return fmt.Errorf("encode venue %q provenance: %w", venue.ID, err)
		}
		var latitude, longitude any
		if venue.Coordinates.Valid() && (venue.Coordinates.Latitude != 0 || venue.Coordinates.Longitude != 0) {
			latitude = venue.Coordinates.Latitude
			longitude = venue.Coordinates.Longitude
		}
		now := timestamp(time.Now())
		if _, err := tx.ExecContext(ctx, `INSERT INTO venues(id, name, address, postal_code, latitude, longitude, classification, sports_json, amenities_json, indoor, sheltered, booking_urls_json, provenance_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, address = excluded.address, postal_code = excluded.postal_code,
latitude = excluded.latitude, longitude = excluded.longitude, classification = excluded.classification,
sports_json = excluded.sports_json, amenities_json = excluded.amenities_json, indoor = excluded.indoor,
sheltered = excluded.sheltered, booking_urls_json = excluded.booking_urls_json, provenance_json = excluded.provenance_json,
updated_at = excluded.updated_at`, venue.ID, venue.Name, venue.Address, venue.PostalCode, latitude, longitude,
			venue.Classification, `["badminton"]`, string(amenities), nullableBool(venue.Indoor), nullableBool(venue.Sheltered), string(bookingURLs), string(provenance), now, now); err != nil {
			return fmt.Errorf("upsert venue %q: %w", venue.ID, err)
		}
		for _, sourceVenueID := range venue.SourceIDs {
			if sourceVenueID == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO venue_sources(venue_id, source_id, source_venue_id)
VALUES (?, ?, ?)
ON CONFLICT(venue_id, source_id) DO UPDATE SET source_venue_id = excluded.source_venue_id`, venue.ID, venue.Provenance.SourceID, sourceVenueID); err != nil {
				return fmt.Errorf("upsert source mapping for venue %q: %w", venue.ID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit venues transaction: %w", err)
	}
	return nil
}

func (store *Store) SearchVenues(ctx context.Context, filter VenueFilter) ([]domain.Venue, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, name, address, postal_code, latitude, longitude, classification, sports_json, amenities_json,
indoor, sheltered, booking_urls_json, provenance_json FROM venues`
	args := []any{}
	if search := strings.TrimSpace(filter.Search); search != "" {
		query += " WHERE lower(name) LIKE lower(?) OR lower(address) LIKE lower(?)"
		like := "%" + search + "%"
		args = append(args, like, like)
	}
	query += " ORDER BY name, id LIMIT ?"
	args = append(args, limit)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search venues: %w", err)
	}
	defer rows.Close()
	venues := []domain.Venue{}
	for rows.Next() {
		venue, err := scanVenue(rows)
		if err != nil {
			return nil, err
		}
		venues = append(venues, venue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate venues: %w", err)
	}
	return venues, nil
}

func (store *Store) GetVenue(ctx context.Context, venueID string) (domain.Venue, error) {
	row := store.db.QueryRowContext(ctx, `SELECT id, name, address, postal_code, latitude, longitude, classification,
sports_json, amenities_json, indoor, sheltered, booking_urls_json, provenance_json FROM venues WHERE id = ?`, venueID)
	venue, err := scanVenue(row)
	if err == sql.ErrNoRows {
		return domain.Venue{}, fmt.Errorf("venue %q does not exist", venueID)
	}
	if err != nil {
		return domain.Venue{}, err
	}
	return venue, nil
}

type venueRowScanner interface {
	Scan(...any) error
}

func scanVenue(row venueRowScanner) (domain.Venue, error) {
	var venue domain.Venue
	var latitude, longitude sql.NullFloat64
	var ignoredSports, amenities, bookingURLs, provenance string
	var indoor, sheltered sql.NullInt64
	if err := row.Scan(&venue.ID, &venue.Name, &venue.Address, &venue.PostalCode, &latitude, &longitude, &venue.Classification,
		&ignoredSports, &amenities, &indoor, &sheltered, &bookingURLs, &provenance); err != nil {
		return domain.Venue{}, err
	}
	if latitude.Valid && longitude.Valid {
		venue.Coordinates = domain.Coordinates{Latitude: latitude.Float64, Longitude: longitude.Float64}
	}
	if err := json.Unmarshal([]byte(amenities), &venue.Amenities); err != nil {
		return domain.Venue{}, fmt.Errorf("decode venue %q amenities: %w", venue.ID, err)
	}
	if err := json.Unmarshal([]byte(bookingURLs), &venue.BookingURLs); err != nil {
		return domain.Venue{}, fmt.Errorf("decode venue %q booking URLs: %w", venue.ID, err)
	}
	if err := json.Unmarshal([]byte(provenance), &venue.Provenance); err != nil {
		return domain.Venue{}, fmt.Errorf("decode venue %q provenance: %w", venue.ID, err)
	}
	venue.Indoor = boolPointer(indoor)
	venue.Sheltered = boolPointer(sheltered)
	return venue, nil
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	if *value {
		return 1
	}
	return 0
}

func boolPointer(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	result := value.Int64 == 1
	return &result
}
