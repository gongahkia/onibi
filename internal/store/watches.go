package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
)

func (store *Store) CreateWatch(ctx context.Context, watch domain.Watch) (domain.Watch, error) {
	if watch.ID == "" || watch.Name == "" || watch.Trigger.Type == "" {
		return domain.Watch{}, errors.New("watch id, name, and trigger are required")
	}
	queryJSON, err := json.Marshal(watch.Query)
	if err != nil {
		return domain.Watch{}, fmt.Errorf("encode watch %q query: %w", watch.ID, err)
	}
	triggerJSON, err := json.Marshal(watch.Trigger)
	if err != nil {
		return domain.Watch{}, fmt.Errorf("encode watch %q trigger: %w", watch.ID, err)
	}
	targetsJSON, err := json.Marshal(watch.Targets)
	if err != nil {
		return domain.Watch{}, fmt.Errorf("encode watch %q targets: %w", watch.ID, err)
	}
	now := time.Now().UTC()
	if watch.CreatedAt.IsZero() {
		watch.CreatedAt = now
	}
	watch.UpdatedAt = now
	_, err = store.db.ExecContext(ctx, `INSERT INTO watches(id, name, query_json, trigger_json, targets_json, enabled, one_shot, expires_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, watch.ID, watch.Name, string(queryJSON), string(triggerJSON), string(targetsJSON), boolInteger(watch.Enabled), boolInteger(watch.OneShot), nullableTime(watch.ExpiresAt), timestamp(watch.CreatedAt), timestamp(watch.UpdatedAt))
	if err != nil {
		return domain.Watch{}, fmt.Errorf("create watch %q: %w", watch.ID, err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO watch_state(watch_id, state_json) VALUES (?, '{}')`, watch.ID); err != nil {
		return domain.Watch{}, fmt.Errorf("create watch state %q: %w", watch.ID, err)
	}
	return watch, nil
}

func (store *Store) ListWatches(ctx context.Context, enabledOnly bool) ([]domain.Watch, error) {
	query := `SELECT id, name, query_json, trigger_json, targets_json, enabled, one_shot, expires_at, created_at, updated_at FROM watches`
	args := []any{}
	if enabledOnly {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY created_at, id"
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list watches: %w", err)
	}
	defer rows.Close()
	result := []domain.Watch{}
	for rows.Next() {
		watch, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, watch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watches: %w", err)
	}
	return result, nil
}

func (store *Store) GetWatch(ctx context.Context, watchID string) (domain.Watch, error) {
	row := store.db.QueryRowContext(ctx, `SELECT id, name, query_json, trigger_json, targets_json, enabled, one_shot, expires_at, created_at, updated_at FROM watches WHERE id = ?`, watchID)
	watch, err := scanWatch(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Watch{}, fmt.Errorf("watch %q does not exist", watchID)
	}
	return watch, err
}

func (store *Store) SetWatchEnabled(ctx context.Context, watchID string, enabled bool) error {
	result, err := store.db.ExecContext(ctx, `UPDATE watches SET enabled = ?, updated_at = ? WHERE id = ?`, boolInteger(enabled), timestamp(time.Now()), watchID)
	if err != nil {
		return fmt.Errorf("set watch %q enabled: %w", watchID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read watch update result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("watch %q does not exist", watchID)
	}
	return nil
}

func (store *Store) DeleteWatch(ctx context.Context, watchID string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM watches WHERE id = ?`, watchID)
	if err != nil {
		return fmt.Errorf("delete watch %q: %w", watchID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read watch delete result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("watch %q does not exist", watchID)
	}
	return nil
}

func (store *Store) GetWatchState(ctx context.Context, watchID string) (domain.WatchState, error) {
	row := store.db.QueryRowContext(ctx, `SELECT watch_id, last_evaluated_at, last_match_at, next_evaluation_at, last_error, state_json FROM watch_state WHERE watch_id = ?`, watchID)
	state, err := scanWatchState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WatchState{}, fmt.Errorf("watch state %q does not exist", watchID)
	}
	return state, err
}

func (store *Store) SaveWatchState(ctx context.Context, state domain.WatchState) error {
	if state.WatchID == "" {
		return errors.New("watch state needs a watch ID")
	}
	if len(state.State) == 0 {
		state.State = json.RawMessage(`{}`)
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO watch_state(watch_id, last_evaluated_at, last_match_at, next_evaluation_at, last_error, state_json)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(watch_id) DO UPDATE SET last_evaluated_at = excluded.last_evaluated_at, last_match_at = excluded.last_match_at,
next_evaluation_at = excluded.next_evaluation_at, last_error = excluded.last_error, state_json = excluded.state_json`, state.WatchID,
		nullableTime(state.LastEvaluatedAt), nullableTime(state.LastMatchAt), nullableTime(state.NextEvaluationAt), state.LastError, string(state.State))
	if err != nil {
		return fmt.Errorf("save watch state %q: %w", state.WatchID, err)
	}
	return nil
}

func (store *Store) InsertEvent(ctx context.Context, event domain.Event) (bool, error) {
	if event.ID == "" || event.Fingerprint == "" || event.WatchID == "" || event.Type == "" || len(event.Payload) == 0 {
		return false, errors.New("event id, fingerprint, watch ID, type, and payload are required")
	}
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = event.ObservedAt
	}
	result, err := store.db.ExecContext(ctx, `INSERT INTO events(id, fingerprint, watch_id, type, payload_json, observed_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(fingerprint) DO NOTHING`, event.ID, event.Fingerprint, event.WatchID, event.Type, string(event.Payload), timestamp(event.ObservedAt), timestamp(event.CreatedAt))
	if err != nil {
		return false, fmt.Errorf("insert event %q: %w", event.ID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read event insert result: %w", err)
	}
	return rows == 1, nil
}

func (store *Store) ListEvents(ctx context.Context, watchID string, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > 1_000 {
		limit = 100
	}
	query := `SELECT id, fingerprint, watch_id, type, payload_json, observed_at, created_at FROM events`
	args := []any{}
	if watchID != "" {
		query += " WHERE watch_id = ?"
		args = append(args, watchID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	events := []domain.Event{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

type watchRowScanner interface{ Scan(...any) error }

func scanWatch(row watchRowScanner) (domain.Watch, error) {
	var watch domain.Watch
	var queryJSON, triggerJSON, targetsJSON string
	var enabled, oneShot int
	var expires sql.NullString
	var created, updated string
	if err := row.Scan(&watch.ID, &watch.Name, &queryJSON, &triggerJSON, &targetsJSON, &enabled, &oneShot, &expires, &created, &updated); err != nil {
		return domain.Watch{}, err
	}
	if err := json.Unmarshal([]byte(queryJSON), &watch.Query); err != nil {
		return domain.Watch{}, fmt.Errorf("decode watch %q query: %w", watch.ID, err)
	}
	if err := json.Unmarshal([]byte(triggerJSON), &watch.Trigger); err != nil {
		return domain.Watch{}, fmt.Errorf("decode watch %q trigger: %w", watch.ID, err)
	}
	if err := json.Unmarshal([]byte(targetsJSON), &watch.Targets); err != nil {
		return domain.Watch{}, fmt.Errorf("decode watch %q targets: %w", watch.ID, err)
	}
	var err error
	watch.ExpiresAt, err = parseNullableTime(expires)
	if err != nil {
		return domain.Watch{}, err
	}
	if watch.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.Watch{}, fmt.Errorf("parse watch %q created time: %w", watch.ID, err)
	}
	if watch.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return domain.Watch{}, fmt.Errorf("parse watch %q updated time: %w", watch.ID, err)
	}
	watch.Enabled = enabled == 1
	watch.OneShot = oneShot == 1
	return watch, nil
}

type watchStateRowScanner interface{ Scan(...any) error }

func scanWatchState(row watchStateRowScanner) (domain.WatchState, error) {
	var state domain.WatchState
	var evaluated, matched, next sql.NullString
	var stored string
	if err := row.Scan(&state.WatchID, &evaluated, &matched, &next, &state.LastError, &stored); err != nil {
		return domain.WatchState{}, err
	}
	var err error
	if state.LastEvaluatedAt, err = parseNullableTime(evaluated); err != nil {
		return domain.WatchState{}, err
	}
	if state.LastMatchAt, err = parseNullableTime(matched); err != nil {
		return domain.WatchState{}, err
	}
	if state.NextEvaluationAt, err = parseNullableTime(next); err != nil {
		return domain.WatchState{}, err
	}
	state.State = json.RawMessage(stored)
	return state, nil
}

type eventRowScanner interface{ Scan(...any) error }

func scanEvent(row eventRowScanner) (domain.Event, error) {
	var event domain.Event
	var payload, observed, created string
	if err := row.Scan(&event.ID, &event.Fingerprint, &event.WatchID, &event.Type, &payload, &observed, &created); err != nil {
		return domain.Event{}, err
	}
	var err error
	if event.ObservedAt, err = time.Parse(time.RFC3339Nano, observed); err != nil {
		return domain.Event{}, fmt.Errorf("parse event %q observed time: %w", event.ID, err)
	}
	if event.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.Event{}, fmt.Errorf("parse event %q created time: %w", event.ID, err)
	}
	event.Payload = json.RawMessage(payload)
	return event, nil
}
