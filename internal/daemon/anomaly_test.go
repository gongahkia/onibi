package daemon

import (
	"testing"
	"time"
)

func TestAnomalyTrackerWarnsAndThrottles(t *testing.T) {
	tr := newAnomalyTracker()
	now := time.Unix(1000, 0)
	for i := 0; i < 7; i++ {
		if _, warn := tr.Record("approval.request", now.Add(time.Duration(i)*time.Second)); warn {
			t.Fatalf("warned early at %d", i)
		}
	}
	if _, warn := tr.Record("approval.request", now.Add(8*time.Second)); !warn {
		t.Fatal("expected warning")
	}
	if _, warn := tr.Record("approval.request", now.Add(9*time.Second)); warn {
		t.Fatal("expected throttle")
	}
	for i := 0; i < 7; i++ {
		tr.Record("approval.request", now.Add(31*time.Minute+time.Duration(i)*time.Second))
	}
	if _, warn := tr.Record("approval.request", now.Add(31*time.Minute+8*time.Second)); !warn {
		t.Fatal("expected warning after throttle window")
	}
}

func TestAnomalyTrackerWindowExpires(t *testing.T) {
	tr := newAnomalyTracker()
	now := time.Unix(1000, 0)
	for i := 0; i < 7; i++ {
		tr.Record("telegram.inject", now.Add(time.Duration(i)*time.Second))
	}
	if _, warn := tr.Record("telegram.inject", now.Add(6*time.Minute)); warn {
		t.Fatal("old events should not count")
	}
}
