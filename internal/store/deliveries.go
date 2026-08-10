package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
)

type DeliveryWithEvent struct {
	Delivery domain.NotificationDelivery `json:"delivery"`
	Event    domain.Event                `json:"event"`
}

func (store *Store) CreateDelivery(ctx context.Context, delivery domain.NotificationDelivery) (domain.NotificationDelivery, bool, error) {
	if delivery.ID == "" || delivery.EventID == "" || delivery.TargetID == "" {
		return domain.NotificationDelivery{}, false, errors.New("delivery id, event ID, and target ID are required")
	}
	if delivery.Status == "" {
		delivery.Status = domain.DeliveryPending
	}
	now := time.Now().UTC()
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	delivery.UpdatedAt = now
	result, err := store.db.ExecContext(ctx, `INSERT INTO notification_deliveries(id, event_id, target_id, status, attempts, last_attempt_at, delivered_at, error, response_code, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(event_id, target_id) DO NOTHING`, delivery.ID, delivery.EventID, delivery.TargetID, delivery.Status, delivery.Attempts, nullableTime(delivery.LastAttemptAt), nullableTime(delivery.DeliveredAt), delivery.Error, nullableInt(delivery.ResponseCode), timestamp(delivery.CreatedAt), timestamp(delivery.UpdatedAt))
	if err != nil {
		return domain.NotificationDelivery{}, false, fmt.Errorf("create delivery %q: %w", delivery.ID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.NotificationDelivery{}, false, fmt.Errorf("read delivery create result: %w", err)
	}
	if rows == 1 {
		return delivery, true, nil
	}
	existing, err := store.GetDelivery(ctx, delivery.EventID, delivery.TargetID)
	return existing, false, err
}

func (store *Store) GetDelivery(ctx context.Context, eventID, targetID string) (domain.NotificationDelivery, error) {
	row := store.db.QueryRowContext(ctx, `SELECT id, event_id, target_id, status, attempts, last_attempt_at, delivered_at, error, response_code, created_at, updated_at FROM notification_deliveries WHERE event_id = ? AND target_id = ?`, eventID, targetID)
	delivery, err := scanDelivery(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NotificationDelivery{}, fmt.Errorf("delivery for event %q and target %q does not exist", eventID, targetID)
	}
	return delivery, err
}

func (store *Store) SaveDeliveryAttempt(ctx context.Context, eventID, targetID string, status domain.DeliveryStatus, responseCode *int, deliveryError string, attemptedAt time.Time) error {
	if status != domain.DeliveryDelivered && status != domain.DeliveryFailed {
		return fmt.Errorf("delivery status %q cannot record an attempt", status)
	}
	var deliveredAt any
	if status == domain.DeliveryDelivered {
		deliveredAt = timestamp(attemptedAt)
	}
	result, err := store.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, attempts = attempts + 1, last_attempt_at = ?, delivered_at = ?, error = ?, response_code = ?, updated_at = ?
WHERE event_id = ? AND target_id = ?`, status, timestamp(attemptedAt), deliveredAt, deliveryError, nullableInt(responseCode), timestamp(time.Now()), eventID, targetID)
	if err != nil {
		return fmt.Errorf("save delivery attempt for event %q: %w", eventID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delivery attempt update result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("delivery for event %q and target %q does not exist", eventID, targetID)
	}
	return nil
}

func (store *Store) ListDeliveries(ctx context.Context, eventID string, limit int) ([]domain.NotificationDelivery, error) {
	if limit <= 0 || limit > 1_000 {
		limit = 100
	}
	query := `SELECT id, event_id, target_id, status, attempts, last_attempt_at, delivered_at, error, response_code, created_at, updated_at FROM notification_deliveries`
	args := []any{}
	if eventID != "" {
		query += " WHERE event_id = ?"
		args = append(args, eventID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	deliveries := []domain.NotificationDelivery{}
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}
	return deliveries, nil
}

func (store *Store) ListRetryableDeliveries(ctx context.Context, maxAttempts, limit int) ([]DeliveryWithEvent, error) {
	if maxAttempts < 1 {
		maxAttempts = 5
	}
	if limit <= 0 || limit > 1_000 {
		limit = 100
	}
	rows, err := store.db.QueryContext(ctx, `SELECT d.id, d.event_id, d.target_id, d.status, d.attempts, d.last_attempt_at, d.delivered_at, d.error, d.response_code, d.created_at, d.updated_at,
e.id, e.fingerprint, e.watch_id, e.type, e.payload_json, e.observed_at, e.created_at
FROM notification_deliveries d JOIN events e ON e.id = d.event_id
WHERE d.status IN (?, ?) AND d.attempts < ? ORDER BY d.updated_at, d.id LIMIT ?`, domain.DeliveryPending, domain.DeliveryFailed, maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("list retryable deliveries: %w", err)
	}
	defer rows.Close()
	result := []DeliveryWithEvent{}
	for rows.Next() {
		item, err := scanDeliveryWithEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retryable deliveries: %w", err)
	}
	return result, nil
}

type deliveryRowScanner interface{ Scan(...any) error }

func scanDelivery(row deliveryRowScanner) (domain.NotificationDelivery, error) {
	var delivery domain.NotificationDelivery
	var lastAttempt, delivered sql.NullString
	var code sql.NullInt64
	var created, updated string
	if err := row.Scan(&delivery.ID, &delivery.EventID, &delivery.TargetID, &delivery.Status, &delivery.Attempts, &lastAttempt, &delivered, &delivery.Error, &code, &created, &updated); err != nil {
		return domain.NotificationDelivery{}, err
	}
	var err error
	if delivery.LastAttemptAt, err = parseNullableTime(lastAttempt); err != nil {
		return domain.NotificationDelivery{}, err
	}
	if delivery.DeliveredAt, err = parseNullableTime(delivered); err != nil {
		return domain.NotificationDelivery{}, err
	}
	if code.Valid {
		value := int(code.Int64)
		delivery.ResponseCode = &value
	}
	if delivery.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.NotificationDelivery{}, fmt.Errorf("parse delivery %q created time: %w", delivery.ID, err)
	}
	if delivery.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return domain.NotificationDelivery{}, fmt.Errorf("parse delivery %q updated time: %w", delivery.ID, err)
	}
	return delivery, nil
}

func scanDeliveryWithEvent(row deliveryRowScanner) (DeliveryWithEvent, error) {
	var item DeliveryWithEvent
	var lastAttempt, delivered sql.NullString
	var code sql.NullInt64
	var deliveryCreated, deliveryUpdated string
	var payload, observed, eventCreated string
	if err := row.Scan(&item.Delivery.ID, &item.Delivery.EventID, &item.Delivery.TargetID, &item.Delivery.Status, &item.Delivery.Attempts, &lastAttempt, &delivered, &item.Delivery.Error, &code, &deliveryCreated, &deliveryUpdated,
		&item.Event.ID, &item.Event.Fingerprint, &item.Event.WatchID, &item.Event.Type, &payload, &observed, &eventCreated); err != nil {
		return DeliveryWithEvent{}, err
	}
	var err error
	if item.Delivery.LastAttemptAt, err = parseNullableTime(lastAttempt); err != nil {
		return DeliveryWithEvent{}, err
	}
	if item.Delivery.DeliveredAt, err = parseNullableTime(delivered); err != nil {
		return DeliveryWithEvent{}, err
	}
	if code.Valid {
		value := int(code.Int64)
		item.Delivery.ResponseCode = &value
	}
	if item.Delivery.CreatedAt, err = time.Parse(time.RFC3339Nano, deliveryCreated); err != nil {
		return DeliveryWithEvent{}, err
	}
	if item.Delivery.UpdatedAt, err = time.Parse(time.RFC3339Nano, deliveryUpdated); err != nil {
		return DeliveryWithEvent{}, err
	}
	if item.Event.ObservedAt, err = time.Parse(time.RFC3339Nano, observed); err != nil {
		return DeliveryWithEvent{}, err
	}
	if item.Event.CreatedAt, err = time.Parse(time.RFC3339Nano, eventCreated); err != nil {
		return DeliveryWithEvent{}, err
	}
	item.Event.Payload = []byte(payload)
	return item, nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
