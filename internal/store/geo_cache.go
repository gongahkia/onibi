package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gongahkia/courtsg/internal/geo"
)

func (store *Store) GetGeocodeCache(ctx context.Context, key string, now time.Time) (geo.Place, bool, error) {
	var place geo.Place
	var expires string
	err := store.db.QueryRowContext(ctx, `SELECT formatted_address, latitude, longitude, provider, expires_at
FROM geocode_cache WHERE key = ?`, key).Scan(&place.Address, &place.Coordinates.Latitude, &place.Coordinates.Longitude, &place.Provider, &expires)
	if err == sql.ErrNoRows {
		return geo.Place{}, false, nil
	}
	if err != nil {
		return geo.Place{}, false, fmt.Errorf("read geocode cache: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return geo.Place{}, false, fmt.Errorf("parse geocode cache expiry: %w", err)
	}
	if !now.Before(expiresAt) {
		return geo.Place{}, false, nil
	}
	return place, true, nil
}

func (store *Store) SaveGeocodeCache(ctx context.Context, key, query string, place geo.Place, expiresAt time.Time) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO geocode_cache(key, query, latitude, longitude, formatted_address, provider, fetched_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET query = excluded.query, latitude = excluded.latitude, longitude = excluded.longitude,
formatted_address = excluded.formatted_address, provider = excluded.provider, fetched_at = excluded.fetched_at, expires_at = excluded.expires_at`,
		key, query, place.Coordinates.Latitude, place.Coordinates.Longitude, place.Address, place.Provider, timestamp(time.Now()), timestamp(expiresAt))
	if err != nil {
		return fmt.Errorf("save geocode cache: %w", err)
	}
	return nil
}

func (store *Store) GetRouteCache(ctx context.Context, key string, now time.Time) (geo.Route, bool, error) {
	var route geo.Route
	var mode, expires string
	var durationSeconds int64
	var fallback int
	err := store.db.QueryRowContext(ctx, `SELECT origin_latitude, origin_longitude, destination_latitude, destination_longitude,
mode, duration_seconds, distance_meters, provider, fallback, expires_at FROM route_cache WHERE key = ?`, key).Scan(
		&route.Origin.Latitude, &route.Origin.Longitude, &route.Destination.Latitude, &route.Destination.Longitude,
		&mode, &durationSeconds, &route.DistanceMeters, &route.Provider, &fallback, &expires)
	if err == sql.ErrNoRows {
		return geo.Route{}, false, nil
	}
	if err != nil {
		return geo.Route{}, false, fmt.Errorf("read route cache: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return geo.Route{}, false, fmt.Errorf("parse route cache expiry: %w", err)
	}
	if !now.Before(expiresAt) {
		return geo.Route{}, false, nil
	}
	route.Mode = geo.TravelMode(mode)
	route.Duration = time.Duration(durationSeconds) * time.Second
	route.Fallback = fallback == 1
	return route, true, nil
}

func (store *Store) SaveRouteCache(ctx context.Context, key string, route geo.Route, expiresAt time.Time) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO route_cache(key, origin_latitude, origin_longitude, destination_latitude, destination_longitude, mode, duration_seconds, distance_meters, provider, fallback, fetched_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET duration_seconds = excluded.duration_seconds, distance_meters = excluded.distance_meters,
provider = excluded.provider, fallback = excluded.fallback, fetched_at = excluded.fetched_at, expires_at = excluded.expires_at`,
		key, route.Origin.Latitude, route.Origin.Longitude, route.Destination.Latitude, route.Destination.Longitude,
		route.Mode, int64(route.Duration.Seconds()), route.DistanceMeters, route.Provider, boolInteger(route.Fallback), timestamp(time.Now()), timestamp(expiresAt))
	if err != nil {
		return fmt.Errorf("save route cache: %w", err)
	}
	return nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
