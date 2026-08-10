package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/query"
)

type WatchEvaluation struct {
	WatchID       string `json:"watch_id"`
	Matches       int    `json:"matches"`
	EventsCreated int    `json:"events_created"`
	State         string `json:"state"`
	Detail        string `json:"detail,omitempty"`
}

type availabilityMatchEvent struct {
	SchemaVersion string              `json:"schema_version"`
	EventType     string              `json:"event_type"`
	WatchID       string              `json:"watch_id"`
	WatchName     string              `json:"watch_name"`
	Result        domain.SearchResult `json:"result"`
}

func (service *Service) CreateWatch(ctx context.Context, input domain.Watch) (domain.Watch, error) {
	if strings.TrimSpace(input.Name) == "" {
		return domain.Watch{}, errors.New("watch name cannot be empty")
	}
	criteria, err := query.Normalize(input.Query)
	if err != nil {
		return domain.Watch{}, err
	}
	input.Query = criteria
	if input.ID == "" {
		input.ID, err = randomID("watch")
		if err != nil {
			return domain.Watch{}, err
		}
	}
	if input.Trigger.Type == "" {
		input.Trigger.Type = domain.WatchTriggerAvailabilityMatch
	}
	if input.Trigger.Type != domain.WatchTriggerAvailabilityMatch {
		return domain.Watch{}, fmt.Errorf("unsupported watch trigger %q", input.Trigger.Type)
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return domain.Watch{}, errors.New("watch expiry must be in the future")
	}
	for index := range input.Targets {
		target := &input.Targets[index]
		switch target.Kind {
		case domain.NotificationTelegram:
			if target.ChatID == 0 {
				return domain.Watch{}, fmt.Errorf("telegram target %d needs a chat ID", index+1)
			}
			if target.ID == "" {
				target.ID = fmt.Sprintf("telegram:%d", target.ChatID)
			}
		case domain.NotificationWebhook:
			target.WebhookName = strings.TrimSpace(target.WebhookName)
			if target.WebhookName == "" {
				return domain.Watch{}, fmt.Errorf("webhook target %d needs a configured webhook name", index+1)
			}
			if target.ID == "" {
				target.ID = "webhook:" + target.WebhookName
			}
		default:
			return domain.Watch{}, fmt.Errorf("unsupported notification target %q", target.Kind)
		}
	}
	return service.store.CreateWatch(ctx, input)
}

func (service *Service) Watches(ctx context.Context, enabledOnly bool) ([]domain.Watch, error) {
	return service.store.ListWatches(ctx, enabledOnly)
}

func (service *Service) Watch(ctx context.Context, watchID string) (domain.Watch, error) {
	return service.store.GetWatch(ctx, watchID)
}

func (service *Service) WatchState(ctx context.Context, watchID string) (domain.WatchState, error) {
	return service.store.GetWatchState(ctx, watchID)
}

func (service *Service) SetWatchEnabled(ctx context.Context, watchID string, enabled bool) error {
	return service.store.SetWatchEnabled(ctx, watchID, enabled)
}

func (service *Service) DeleteWatch(ctx context.Context, watchID string) error {
	return service.store.DeleteWatch(ctx, watchID)
}

func (service *Service) Events(ctx context.Context, watchID string, limit int) ([]domain.Event, error) {
	return service.store.ListEvents(ctx, watchID, limit)
}

// EvaluateWatches reads only local availability. Network refresh is intentionally
// a separate phase, preventing each watch from multiplying upstream requests.
func (service *Service) EvaluateWatches(ctx context.Context) ([]WatchEvaluation, error) {
	watches, err := service.store.ListWatches(ctx, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	results := make([]WatchEvaluation, 0, len(watches))
	for _, watch := range watches {
		result := WatchEvaluation{WatchID: watch.ID, State: "evaluated"}
		if watch.ExpiresAt != nil && !watch.ExpiresAt.After(now) {
			if err := service.store.SetWatchEnabled(ctx, watch.ID, false); err != nil {
				return results, err
			}
			result.State = "expired"
			result.Detail = "watch expiry reached"
			results = append(results, result)
			continue
		}
		matches, err := service.Search(ctx, watch.Query)
		if err != nil {
			stateErr := service.store.SaveWatchState(ctx, domain.WatchState{WatchID: watch.ID, LastEvaluatedAt: &now, NextEvaluationAt: nextWatchEvaluation(service, now), LastError: err.Error()})
			if stateErr != nil {
				return results, stateErr
			}
			result.State = "error"
			result.Detail = err.Error()
			results = append(results, result)
			continue
		}
		result.Matches = len(matches)
		var lastMatch *time.Time
		for _, match := range matches {
			payload, err := json.Marshal(availabilityMatchEvent{SchemaVersion: "v1", EventType: domain.WatchTriggerAvailabilityMatch, WatchID: watch.ID, WatchName: watch.Name, Result: match})
			if err != nil {
				return results, fmt.Errorf("encode watch %q event: %w", watch.ID, err)
			}
			fingerprint := eventFingerprint(watch.ID, match)
			event := domain.Event{ID: "event:" + fingerprint[:24], Fingerprint: fingerprint, WatchID: watch.ID, Type: domain.WatchTriggerAvailabilityMatch, Payload: payload, ObservedAt: now, CreatedAt: now}
			created, err := service.store.InsertEvent(ctx, event)
			if err != nil {
				return results, err
			}
			if created {
				result.EventsCreated++
				if err := service.queueEventDeliveries(ctx, event, watch.Targets); err != nil {
					return results, err
				}
			}
		}
		if len(matches) > 0 {
			matched := now
			lastMatch = &matched
		}
		statePayload, err := json.Marshal(map[string]int{"match_count": result.Matches, "events_created": result.EventsCreated})
		if err != nil {
			return results, err
		}
		if err := service.store.SaveWatchState(ctx, domain.WatchState{WatchID: watch.ID, LastEvaluatedAt: &now, LastMatchAt: lastMatch, NextEvaluationAt: nextWatchEvaluation(service, now), State: statePayload}); err != nil {
			return results, err
		}
		if watch.OneShot && result.EventsCreated > 0 {
			if err := service.store.SetWatchEnabled(ctx, watch.ID, false); err != nil {
				return results, err
			}
			result.State = "triggered_one_shot"
		}
		results = append(results, result)
	}
	return results, nil
}

func (service *Service) Deliveries(ctx context.Context, eventID string, limit int) ([]domain.NotificationDelivery, error) {
	return service.store.ListDeliveries(ctx, eventID, limit)
}

// RetryDeliveries retries durable failed/pending deliveries without generating
// another event, preserving the original event ID and idempotency key.
func (service *Service) RetryDeliveries(ctx context.Context, maxAttempts, limit int) ([]domain.NotificationDelivery, error) {
	work, err := service.store.ListRetryableDeliveries(ctx, maxAttempts, limit)
	if err != nil {
		return nil, err
	}
	result := make([]domain.NotificationDelivery, 0, len(work))
	for _, item := range work {
		watch, err := service.store.GetWatch(ctx, item.Event.WatchID)
		if err != nil {
			return result, err
		}
		target, ok := findTarget(watch.Targets, item.Delivery.TargetID)
		if !ok {
			if err := service.store.SaveDeliveryAttempt(ctx, item.Event.ID, item.Delivery.TargetID, domain.DeliveryFailed, nil, "target no longer exists on watch", time.Now().UTC()); err != nil {
				return result, err
			}
		} else if err := service.attemptDelivery(ctx, item.Event, target); err != nil {
			return result, err
		}
		delivery, err := service.store.GetDelivery(ctx, item.Event.ID, item.Delivery.TargetID)
		if err != nil {
			return result, err
		}
		result = append(result, delivery)
	}
	return result, nil
}

func (service *Service) queueEventDeliveries(ctx context.Context, event domain.Event, targets []domain.NotificationTarget) error {
	for _, target := range targets {
		delivery, created, err := service.store.CreateDelivery(ctx, domain.NotificationDelivery{ID: deliveryID(event.ID, target.ID), EventID: event.ID, TargetID: target.ID})
		if err != nil {
			return err
		}
		if created && delivery.Status == domain.DeliveryPending {
			if err := service.attemptDelivery(ctx, event, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func (service *Service) attemptDelivery(ctx context.Context, event domain.Event, target domain.NotificationTarget) error {
	now := time.Now().UTC()
	if service.notifier == nil {
		return service.store.SaveDeliveryAttempt(ctx, event.ID, target.ID, domain.DeliveryFailed, nil, "notification delivery is not configured", now)
	}
	statusCode, err := service.notifier.Send(ctx, event, target)
	if err != nil {
		var code *int
		if statusCode != 0 {
			code = &statusCode
		}
		return service.store.SaveDeliveryAttempt(ctx, event.ID, target.ID, domain.DeliveryFailed, code, err.Error(), now)
	}
	code := statusCode
	return service.store.SaveDeliveryAttempt(ctx, event.ID, target.ID, domain.DeliveryDelivered, &code, "", now)
}

func findTarget(targets []domain.NotificationTarget, targetID string) (domain.NotificationTarget, bool) {
	for _, target := range targets {
		if target.ID == targetID {
			return target, true
		}
	}
	return domain.NotificationTarget{}, false
}

func deliveryID(eventID, targetID string) string {
	digest := sha256.Sum256([]byte(eventID + "\x00" + targetID))
	return "delivery:" + hex.EncodeToString(digest[:12])
}

func nextWatchEvaluation(service *Service, now time.Time) *time.Time {
	next := now.Add(time.Duration(service.config.Daemon.RefreshMinutes) * time.Minute)
	return &next
}

func eventFingerprint(watchID string, result domain.SearchResult) string {
	price := ""
	if result.Slot.PriceCents != nil {
		price = fmt.Sprintf("%d", *result.Slot.PriceCents)
	}
	value := strings.Join([]string{watchID, result.Slot.SourceID, strings.Join(result.ComponentSlotIDs, ","), result.Slot.ID, result.Slot.Start.UTC().Format(time.RFC3339Nano), result.Slot.End.UTC().Format(time.RFC3339Nano), string(result.Slot.Status), price, result.Slot.BookingURL}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate %s ID: %w", prefix, err)
	}
	return prefix + ":" + hex.EncodeToString(bytes), nil
}
