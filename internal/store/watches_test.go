package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

func TestWatchAndEventPersistenceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kaypoh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	watch, err := store.CreateWatch(ctx, domain.Watch{ID: "watch", Name: "Wednesday badminton", Query: domain.Query{}, Trigger: domain.WatchTrigger{Type: domain.WatchTriggerAvailabilityMatch}, Targets: []domain.NotificationTarget{{ID: "webhook:test", Kind: domain.NotificationWebhook, WebhookName: "test"}}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !watch.Enabled || watch.CreatedAt.IsZero() {
		t.Fatalf("CreateWatch() = %#v", watch)
	}
	payload, err := json.Marshal(map[string]string{"slot": "one"})
	if err != nil {
		t.Fatal(err)
	}
	event := domain.Event{ID: "event", Fingerprint: "fingerprint", WatchID: watch.ID, Type: domain.WatchTriggerAvailabilityMatch, Payload: payload, ObservedAt: time.Now()}
	created, err := store.InsertEvent(ctx, event)
	if err != nil || !created {
		t.Fatalf("first InsertEvent() = %t, %v", created, err)
	}
	created, err = store.InsertEvent(ctx, event)
	if err != nil || created {
		t.Fatalf("second InsertEvent() = %t, %v", created, err)
	}
	events, err := store.ListEvents(ctx, watch.ID, 10)
	if err != nil || len(events) != 1 || events[0].Fingerprint != event.Fingerprint {
		t.Fatalf("ListEvents() = %#v, %v", events, err)
	}
	stateTime := time.Now().UTC()
	if err := store.SaveWatchState(ctx, domain.WatchState{WatchID: watch.ID, LastEvaluatedAt: &stateTime, State: json.RawMessage(`{"result_count":1}`)}); err != nil {
		t.Fatal(err)
	}
	state, err := store.GetWatchState(ctx, watch.ID)
	if err != nil || state.LastEvaluatedAt == nil || string(state.State) != `{"result_count":1}` {
		t.Fatalf("GetWatchState() = %#v, %v", state, err)
	}
}
