package domain

import (
	"encoding/json"
	"time"
)

type NotificationTargetKind string

const (
	NotificationTelegram NotificationTargetKind = "telegram"
	NotificationWebhook  NotificationTargetKind = "webhook"
)

type NotificationTarget struct {
	ID          string                 `json:"id"`
	Kind        NotificationTargetKind `json:"kind"`
	ChatID      int64                  `json:"chat_id,omitempty"`
	WebhookName string                 `json:"webhook_name,omitempty"`
}

type WatchTrigger struct {
	Type string `json:"type"`
}

const WatchTriggerAvailabilityMatch = "availability_match"

type Watch struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Query     Query                `json:"query"`
	Trigger   WatchTrigger         `json:"trigger"`
	Targets   []NotificationTarget `json:"targets"`
	Enabled   bool                 `json:"enabled"`
	OneShot   bool                 `json:"one_shot"`
	ExpiresAt *time.Time           `json:"expires_at,omitempty"`
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
}

type WatchState struct {
	WatchID          string          `json:"watch_id"`
	LastEvaluatedAt  *time.Time      `json:"last_evaluated_at,omitempty"`
	LastMatchAt      *time.Time      `json:"last_match_at,omitempty"`
	NextEvaluationAt *time.Time      `json:"next_evaluation_at,omitempty"`
	LastError        string          `json:"last_error,omitempty"`
	State            json.RawMessage `json:"state,omitempty"`
}

type Event struct {
	ID          string          `json:"id"`
	Fingerprint string          `json:"fingerprint"`
	WatchID     string          `json:"watch_id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	ObservedAt  time.Time       `json:"observed_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

type NotificationDelivery struct {
	ID            string         `json:"id"`
	EventID       string         `json:"event_id"`
	TargetID      string         `json:"target_id"`
	Status        DeliveryStatus `json:"status"`
	Attempts      int            `json:"attempts"`
	LastAttemptAt *time.Time     `json:"last_attempt_at,omitempty"`
	DeliveredAt   *time.Time     `json:"delivered_at,omitempty"`
	Error         string         `json:"error,omitempty"`
	ResponseCode  *int           `json:"response_code,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
