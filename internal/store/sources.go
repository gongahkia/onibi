package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

type SourceRecord struct {
	Info    domain.SourceInfo   `json:"info"`
	Enabled bool                `json:"enabled"`
	Health  domain.SourceHealth `json:"health"`
}

func (store *Store) UpsertSports(ctx context.Context, sports []domain.Sport) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sports transaction: %w", err)
	}
	defer tx.Rollback()
	for _, sport := range sports {
		aliases, err := json.Marshal(sport.Aliases)
		if err != nil {
			return fmt.Errorf("encode sport aliases: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sports(id, name, aliases_json, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, aliases_json = excluded.aliases_json, updated_at = excluded.updated_at`, sport.ID, sport.Name, string(aliases), timestamp(time.Now())); err != nil {
			return fmt.Errorf("upsert sport %q: %w", sport.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sports transaction: %w", err)
	}
	return nil
}

func (store *Store) UpsertSources(ctx context.Context, infos []domain.SourceInfo, enabled func(string) bool) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sources transaction: %w", err)
	}
	defer tx.Rollback()
	for _, info := range infos {
		policy, err := json.Marshal(info.Policy)
		if err != nil {
			return fmt.Errorf("encode source policy %q: %w", info.ID, err)
		}
		isEnabled := 0
		if enabled(info.ID) {
			isEnabled = 1
		}
		now := timestamp(time.Now())
		if _, err := tx.ExecContext(ctx, `INSERT INTO sources(id, name, operator, website, policy_json, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  operator = excluded.operator,
  website = excluded.website,
  policy_json = excluded.policy_json,
  enabled = excluded.enabled,
  updated_at = excluded.updated_at`, info.ID, info.Name, info.Operator, info.Website, string(policy), isEnabled, now, now); err != nil {
			return fmt.Errorf("upsert source %q: %w", info.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sources transaction: %w", err)
	}
	return nil
}

func (store *Store) SetSourceEnabled(ctx context.Context, sourceID string, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	result, err := store.db.ExecContext(ctx, "UPDATE sources SET enabled = ?, updated_at = ? WHERE id = ?", value, timestamp(time.Now()), sourceID)
	if err != nil {
		return fmt.Errorf("set source %q enabled: %w", sourceID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read source update result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("source %q does not exist", sourceID)
	}
	return nil
}

func (store *Store) ListSources(ctx context.Context) ([]SourceRecord, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT s.id, s.name, s.operator, s.website, s.policy_json, s.enabled,
COALESCE(h.state, ''), h.last_attempt, h.last_success, COALESCE(h.last_category, ''),
COALESCE(h.latency_ms, 0), COALESCE(h.records_parsed, 0), COALESCE(h.consecutive_failures, 0),
h.backoff_until, COALESCE(h.last_error, '')
FROM sources s LEFT JOIN source_health h ON h.source_id = s.id ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()
	records := []SourceRecord{}
	for rows.Next() {
		record, err := scanSourceRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sources: %w", err)
	}
	return records, nil
}

func (store *Store) GetSource(ctx context.Context, sourceID string) (SourceRecord, error) {
	row := store.db.QueryRowContext(ctx, `SELECT s.id, s.name, s.operator, s.website, s.policy_json, s.enabled,
COALESCE(h.state, ''), h.last_attempt, h.last_success, COALESCE(h.last_category, ''),
COALESCE(h.latency_ms, 0), COALESCE(h.records_parsed, 0), COALESCE(h.consecutive_failures, 0),
h.backoff_until, COALESCE(h.last_error, '')
FROM sources s LEFT JOIN source_health h ON h.source_id = s.id WHERE s.id = ?`, sourceID)
	record, err := scanSourceRecord(row)
	if err == sql.ErrNoRows {
		return SourceRecord{}, fmt.Errorf("source %q does not exist", sourceID)
	}
	if err != nil {
		return SourceRecord{}, err
	}
	return record, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanSourceRecord(row rowScanner) (SourceRecord, error) {
	var record SourceRecord
	var policyJSON string
	var enabled int
	var state string
	var lastAttempt, lastSuccess, backoff sql.NullString
	if err := row.Scan(&record.Info.ID, &record.Info.Name, &record.Info.Operator, &record.Info.Website, &policyJSON, &enabled,
		&state, &lastAttempt, &lastSuccess, &record.Health.LastCategory, &record.Health.LatencyMilliseconds,
		&record.Health.RecordsParsed, &record.Health.ConsecutiveFailures, &backoff, &record.Health.LastError); err != nil {
		return SourceRecord{}, fmt.Errorf("scan source: %w", err)
	}
	if err := json.Unmarshal([]byte(policyJSON), &record.Info.Policy); err != nil {
		return SourceRecord{}, fmt.Errorf("decode source %q policy: %w", record.Info.ID, err)
	}
	record.Enabled = enabled == 1
	record.Health.SourceID = record.Info.ID
	if state == "" {
		if !record.Info.Policy.AllowsNetwork() || !record.Enabled {
			record.Health.State = domain.HealthDisabled
		} else if record.Info.Policy.AuthRequired {
			record.Health.State = domain.HealthCredentials
		} else {
			record.Health.State = domain.HealthUnknown
		}
	} else {
		record.Health.State = domain.HealthState(state)
	}
	var err error
	if record.Health.LastAttempt, err = parseNullableTime(lastAttempt); err != nil {
		return SourceRecord{}, err
	}
	if record.Health.LastSuccess, err = parseNullableTime(lastSuccess); err != nil {
		return SourceRecord{}, err
	}
	if record.Health.BackoffUntil, err = parseNullableTime(backoff); err != nil {
		return SourceRecord{}, err
	}
	return record, nil
}

func (store *Store) SaveSourceHealth(ctx context.Context, health domain.SourceHealth) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO source_health(source_id, state, last_attempt, last_success, last_category, latency_ms, records_parsed, consecutive_failures, backoff_until, last_error, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_id) DO UPDATE SET state = excluded.state, last_attempt = excluded.last_attempt,
last_success = excluded.last_success, last_category = excluded.last_category, latency_ms = excluded.latency_ms,
records_parsed = excluded.records_parsed, consecutive_failures = excluded.consecutive_failures,
backoff_until = excluded.backoff_until, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		health.SourceID, health.State, nullableTime(health.LastAttempt), nullableTime(health.LastSuccess), health.LastCategory,
		health.LatencyMilliseconds, health.RecordsParsed, health.ConsecutiveFailures, nullableTime(health.BackoffUntil), health.LastError, timestamp(time.Now()))
	if err != nil {
		return fmt.Errorf("save source health %q: %w", health.SourceID, err)
	}
	return nil
}

func timestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return timestamp(*value)
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, fmt.Errorf("parse stored timestamp %q: %w", value.String, err)
	}
	return &parsed, nil
}
