package store

import (
	"context"
	"fmt"
)

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{version: 1, sql: `
CREATE TABLE IF NOT EXISTS sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    operator TEXT NOT NULL,
    website TEXT NOT NULL DEFAULT '',
    policy_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sports (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    aliases_json TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS venues (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT NOT NULL DEFAULT '',
    postal_code TEXT NOT NULL DEFAULT '',
    latitude REAL,
    longitude REAL,
    classification TEXT NOT NULL DEFAULT '',
    sports_json TEXT NOT NULL DEFAULT '[]',
    amenities_json TEXT NOT NULL DEFAULT '[]',
    indoor INTEGER,
    sheltered INTEGER,
    booking_urls_json TEXT NOT NULL DEFAULT '[]',
    provenance_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS venue_sources (
    venue_id TEXT NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    source_venue_id TEXT NOT NULL,
    PRIMARY KEY (venue_id, source_id),
    UNIQUE (source_id, source_venue_id)
);

CREATE TABLE IF NOT EXISTS facilities (
    id TEXT PRIMARY KEY,
    venue_id TEXT NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE RESTRICT,
    source_facility_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    court_type TEXT NOT NULL DEFAULT '',
    sports_json TEXT NOT NULL DEFAULT '[]',
    attributes_json TEXT NOT NULL DEFAULT '[]',
    provenance_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (source_id, source_facility_id)
);

CREATE TABLE IF NOT EXISTS price_rules (
    id TEXT PRIMARY KEY,
    venue_id TEXT NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
    facility_id TEXT REFERENCES facilities(id) ON DELETE CASCADE,
    currency TEXT NOT NULL,
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    unit TEXT NOT NULL,
    peak INTEGER,
    member INTEGER,
    resident INTEGER,
    days_json TEXT NOT NULL DEFAULT '[]',
    start_minute INTEGER,
    end_minute INTEGER,
    effective_from TEXT,
    effective_to TEXT,
    provenance_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS source_observations (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    reference TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    parser TEXT NOT NULL,
    evidence_hash TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL,
    expires_at TEXT,
    UNIQUE (source_id, reference, fetched_at)
);

CREATE TABLE IF NOT EXISTS availability_slots (
    id TEXT PRIMARY KEY,
    sport_id TEXT NOT NULL REFERENCES sports(id) ON DELETE RESTRICT,
    venue_id TEXT NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
    facility_id TEXT REFERENCES facilities(id) ON DELETE SET NULL,
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE RESTRICT,
    start_at TEXT NOT NULL,
    end_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('available', 'unavailable', 'unknown')),
    price_cents INTEGER,
    currency TEXT NOT NULL DEFAULT 'SGD',
    booking_url TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    stale_after TEXT NOT NULL,
    provenance_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (end_at > start_at)
);

CREATE INDEX IF NOT EXISTS availability_lookup_idx
    ON availability_slots (sport_id, start_at, end_at, status, stale_after);
CREATE INDEX IF NOT EXISTS availability_venue_idx
    ON availability_slots (venue_id, start_at);
CREATE INDEX IF NOT EXISTS availability_source_idx
    ON availability_slots (source_id, fetched_at);

CREATE TABLE IF NOT EXISTS geocode_cache (
    key TEXT PRIMARY KEY,
    query TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    formatted_address TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS route_cache (
    key TEXT PRIMARY KEY,
    origin_latitude REAL NOT NULL,
    origin_longitude REAL NOT NULL,
    destination_latitude REAL NOT NULL,
    destination_longitude REAL NOT NULL,
    mode TEXT NOT NULL,
    duration_seconds INTEGER NOT NULL,
    distance_meters INTEGER NOT NULL,
    provider TEXT NOT NULL,
    fallback INTEGER NOT NULL DEFAULT 0 CHECK (fallback IN (0, 1)),
    fetched_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS watches (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    query_json TEXT NOT NULL,
    trigger_json TEXT NOT NULL,
    targets_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    one_shot INTEGER NOT NULL DEFAULT 0 CHECK (one_shot IN (0, 1)),
    expires_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS watch_state (
    watch_id TEXT PRIMARY KEY REFERENCES watches(id) ON DELETE CASCADE,
    last_evaluated_at TEXT,
    last_match_at TEXT,
    next_evaluation_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    state_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    watch_id TEXT NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS events_watch_idx ON events (watch_id, created_at DESC);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TEXT,
    delivered_at TEXT,
    error TEXT NOT NULL DEFAULT '',
    response_code INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (event_id, target_id)
);

CREATE TABLE IF NOT EXISTS source_health (
    source_id TEXT PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    state TEXT NOT NULL,
    last_attempt TEXT,
    last_success TEXT,
    last_category TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    records_parsed INTEGER NOT NULL DEFAULT 0,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    backoff_until TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
`},
	{version: 2, sql: `
ALTER TABLE availability_slots ADD COLUMN membership_required INTEGER;
`},
}

func (store *Store) Migrate(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        applied_at TEXT NOT NULL
    )`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	for _, migration := range migrations {
		var applied bool
		if err := store.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}
		tx, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))", migration.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}
	return nil
}
