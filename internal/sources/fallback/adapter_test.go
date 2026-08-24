package fallback

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

func TestAdapterFallsBackInOrderAndRecordsPriorFailures(t *testing.T) {
	var calls []string
	adapter, err := New(domain.SourceInfo{ID: "source", Name: "Source", Operator: "Operator"}, []Attempt{
		{Mode: "api", Fetch: func(context.Context, source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			calls = append(calls, "api")
			return source.AvailabilitySnapshot{}, errors.New("API is unavailable")
		}},
		{Mode: "browser", Fetch: func(context.Context, source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			calls = append(calls, "browser")
			return source.AvailabilitySnapshot{Slots: []domain.AvailabilitySlot{{ID: "slot"}}}, nil
		}},
		{Mode: "activesg", Fetch: func(context.Context, source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			calls = append(calls, "activesg")
			return source.AvailabilitySnapshot{}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := adapter.FetchSnapshot(context.Background(), source.AvailabilityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Slots) != 1 || !reflect.DeepEqual(calls, []string{"api", "browser"}) {
		t.Fatalf("snapshot = %#v, calls = %#v", snapshot, calls)
	}
	health, err := adapter.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.State != domain.HealthHealthy || health.LastCategory != "browser" || !reflect.DeepEqual(health.AccessFailures, []domain.AccessFailure{{Mode: "api", Error: "API is unavailable"}}) {
		t.Fatalf("health = %#v", health)
	}
}

func TestAdapterReportsEveryFailedMode(t *testing.T) {
	adapter, err := New(domain.SourceInfo{ID: "source", Name: "Source", Operator: "Operator"}, []Attempt{
		{Mode: "api", Fetch: func(context.Context, source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			return source.AvailabilitySnapshot{}, errors.New("API is unavailable")
		}},
		{Mode: "activesg", Fetch: func(context.Context, source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			return source.AvailabilitySnapshot{}, errors.New("session expired")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.FetchSnapshot(context.Background(), source.AvailabilityRequest{}); err == nil {
		t.Fatal("FetchSnapshot() succeeded with no successful access mode")
	}
	health, err := adapter.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.AccessFailure{{Mode: "api", Error: "API is unavailable"}, {Mode: "activesg", Error: "session expired"}}
	if health.State != domain.HealthDegraded || health.LastCategory != "all_access_modes_failed" || !reflect.DeepEqual(health.AccessFailures, want) {
		t.Fatalf("health = %#v, want failures %#v", health, want)
	}
}
