package web

import (
	"testing"

	"github.com/gongahkia/onibi/internal/faulttest"
)

func TestFaultDelayedEventReplayAfterSocketResetHasNoDuplicate(t *testing.T) {
	bus := NewEventBus()
	gate := faulttest.NewGate()
	replay, events, unsubscribe := bus.SubscribeFrom(0)
	defer unsubscribe()
	if len(replay.Events) != 0 || replay.Seq != 0 || replay.Snapshot {
		t.Fatalf("initial replay=%#v", replay)
	}
	published := make(chan struct{})
	go func() {
		if err := gate.Wait(t.Context()); err == nil {
			bus.Publish(Event{Type: "session.activity", Payload: map[string]string{"session_id": "session-fault"}})
		}
		close(published)
	}()
	select {
	case <-gate.Started():
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	select {
	case event := <-events:
		t.Fatalf("event arrived before release: %#v", event)
	default:
	}
	gate.Release()
	select {
	case <-published:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	var event Event
	select {
	case event = <-events:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if event.Seq != 1 || event.Type != "session.activity" {
		t.Fatalf("event=%#v", event)
	}
	replay, afterReset, unsubscribeAfterReset := bus.SubscribeFrom(event.Seq)
	defer unsubscribeAfterReset()
	if len(replay.Events) != 0 || replay.Seq != event.Seq || replay.Snapshot {
		t.Fatalf("replay=%#v", replay)
	}
	select {
	case duplicate := <-afterReset:
		t.Fatalf("duplicate event after reset: %#v", duplicate)
	default:
	}
}
