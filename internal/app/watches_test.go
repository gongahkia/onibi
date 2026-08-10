package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/domain"
)

type fakeNotifier struct{ calls int }

func (notifier *fakeNotifier) Send(_ context.Context, _ domain.Event, _ domain.NotificationTarget) (int, error) {
	notifier.calls++
	return 202, nil
}

func TestEvaluateWatchesCreatesOneIdempotentEvent(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "courtsg.db")
	service, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	notifier := &fakeNotifier{}
	service.notifier = notifier
	now := time.Now().UTC().Truncate(time.Second)
	if err := service.store.UpsertVenues(context.Background(), []domain.Venue{{ID: "venue", Name: "Venue", Coordinates: domain.Coordinates{Latitude: 1.3, Longitude: 103.8}, Provenance: domain.Provenance{SourceID: "local-manual", FetchedAt: now}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ImportManualAvailability(context.Background(), []domain.AvailabilitySlot{{SportID: "tennis", VenueID: "venue", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	watch, err := service.CreateWatch(context.Background(), domain.Watch{ID: "watch", Name: "Tennis", Query: domain.Query{Sports: []string{"tennis"}, MinimumDuration: time.Hour}, Targets: []domain.NotificationTarget{{Kind: domain.NotificationWebhook, WebhookName: "test"}}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.EvaluateWatches(context.Background())
	if err != nil || len(first) != 1 || first[0].EventsCreated != 1 {
		t.Fatalf("first EvaluateWatches() = %#v, %v", first, err)
	}
	second, err := service.EvaluateWatches(context.Background())
	if err != nil || len(second) != 1 || second[0].EventsCreated != 0 {
		t.Fatalf("second EvaluateWatches() = %#v, %v", second, err)
	}
	events, err := service.Events(context.Background(), watch.ID, 10)
	if err != nil || len(events) != 1 || events[0].Type != domain.WatchTriggerAvailabilityMatch {
		t.Fatalf("Events() = %#v, %v", events, err)
	}
	deliveries, err := service.Deliveries(context.Background(), events[0].ID, 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != domain.DeliveryDelivered || deliveries[0].Attempts != 1 || notifier.calls != 1 {
		t.Fatalf("Deliveries() = %#v, %v; notifier calls = %d", deliveries, err, notifier.calls)
	}
}
