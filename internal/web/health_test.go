package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
)

type observingBackend struct {
	*fakeBackend
	list    func(context.Context) ([]ghostty.Target, error)
	version string
}

func (b *observingBackend) Targets(ctx context.Context) ([]ghostty.Target, error) { return b.list(ctx) }
func (b *observingBackend) Capabilities() ghostty.Capabilities {
	return ghostty.CapabilitiesForVersion(b.version)
}

func waitInventoryRefs(t *testing.T, s *Server, want int) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		n := 0
		if s.inventoryFlight != nil {
			n = s.inventoryFlight.refs
		}
		s.mu.Unlock()
		if n == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("inventory waiters=%d want=%d", n, want)
		case <-tick.C:
		}
	}
}

func TestInventoryCoalescesAndCancelsOnlyAfterLastWaiter(t *testing.T) {
	s, base := testServer(t, testOrigin)
	var calls atomic.Int32
	started, canceled := make(chan struct{}), make(chan struct{})
	s.backend = &observingBackend{fakeBackend: base, list: func(ctx context.Context) ([]ghostty.Target, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}}
	first, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	second, cancelSecond := context.WithCancel(t.Context())
	defer cancelSecond()
	done1, done2 := make(chan error, 1), make(chan error, 1)
	go func() { _, err := s.observedTargets(first); done1 <- err }()
	<-started
	go func() { _, err := s.observedTargets(second); done2 <- err }()
	waitInventoryRefs(t, s, 2)
	cancelFirst()
	if !errors.Is(<-done1, context.Canceled) {
		t.Fatal("first waiter did not cancel")
	}
	select {
	case <-canceled:
		t.Fatal("live waiter lost shared work")
	default:
	}
	cancelSecond()
	if !errors.Is(<-done2, context.Canceled) {
		t.Fatal("last waiter did not cancel")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("native read outlived all clients")
	}
	if calls.Load() != 1 {
		t.Fatalf("native calls=%d", calls.Load())
	}
}

func TestInventorySharesResultWithoutSharingMutableSlice(t *testing.T) {
	s, base := testServer(t, testOrigin)
	release := make(chan struct{})
	var calls atomic.Int32
	s.backend = &observingBackend{fakeBackend: base, list: func(context.Context) ([]ghostty.Target, error) { calls.Add(1); <-release; return base.targets, nil }}
	done := make(chan []ghostty.Target, 2)
	for range 2 {
		go func() {
			targets, err := s.observedTargets(t.Context())
			if err != nil {
				t.Error(err)
			}
			done <- targets
		}()
	}
	waitInventoryRefs(t, s, 2)
	close(release)
	first, second := <-done, <-done
	first[0].Title = "changed"
	if second[0].Title == "changed" || base.targets[0].Title == "changed" || calls.Load() != 1 {
		t.Fatal("result aliases shared state or duplicate native reads")
	}
}

func TestNewInventoryDoesNotInheritAbandonedFlight(t *testing.T) {
	s, base := testServer(t, testOrigin)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s.backend = &observingBackend{fakeBackend: base, list: func(context.Context) ([]ghostty.Target, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release // Model a native adapter that is slow to notice cancellation.
			return nil, ghostty.ErrUnresponsive
		}
		return base.targets, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := s.observedTargets(ctx); first <- err }()
	<-started
	s.mu.Lock()
	abandoned := s.inventoryFlight
	s.mu.Unlock()
	cancel()
	if !errors.Is(<-first, context.Canceled) {
		t.Fatal("abandoned waiter did not cancel")
	}
	nextCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	targets, err := s.observedTargets(nextCtx)
	if err != nil || len(targets) != 1 || calls.Load() != 2 {
		t.Fatalf("new client inherited abandoned work: targets=%v err=%v calls=%d", targets, err, calls.Load())
	}
	close(release)
	select {
	case <-abandoned.done:
	case <-nextCtx.Done():
		t.Fatal("abandoned work did not finish")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inventoryObservation.State != "ready" || s.inventoryErr != nil {
		t.Fatal("abandoned result overwrote fresh readiness")
	}
}

func TestInventoryBackoffDoesNotCacheActionPreconditions(t *testing.T) {
	s, base := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	var calls int
	s.backend = &observingBackend{fakeBackend: base, version: "1.3.1", list: func(context.Context) ([]ghostty.Target, error) {
		calls++
		if calls == 1 {
			return nil, ghostty.ErrUnresponsive
		}
		return base.targets, nil
	}}
	first := request(s, b, http.MethodGet, "/api/targets", nil)
	if first.Code != 503 || first.Header().Get("Retry-After") == "" {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	request(s, b, http.MethodGet, "/api/targets", nil)
	if calls != 1 {
		t.Fatal("polls ignored backoff")
	}
	action := request(s, b, http.MethodPost, "/api/actions", inputRequest("fresh-precondition-01", "hello"))
	if decodeReceipt(t, action).State != "completed" || calls != 2 {
		t.Fatal("mutation reused failed/cached inventory")
	}
	if got := request(s, b, http.MethodGet, "/api/targets", nil); got.Code != 200 || calls != 3 {
		t.Fatal("successful fresh validation did not reset backoff")
	}
}

func TestHealthSeparatesCaptureAndInventoryAndRequiresAuthentication(t *testing.T) {
	s, base := testServer(t, testOrigin)
	if got := request(s, browser{}, http.MethodGet, "/api/health", nil); got.Code != 401 {
		t.Fatal("health exposed before pairing")
	}
	b := pairBrowser(t, s)
	request(s, b, http.MethodGet, "/api/targets", nil)
	base.snapErr = ghostty.ErrCapturePermission
	got := request(s, b, http.MethodGet, "/api/snapshot?targetId=surface-1", nil)
	if got.Code != 503 {
		t.Fatal(got.Body)
	}
	var health struct {
		Inventory Observation `json:"inventory"`
		Capture   Observation `json:"capture"`
	}
	got = request(s, b, http.MethodGet, "/api/health", nil)
	if err := json.Unmarshal(got.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Inventory.State != "ready" || health.Capture.Code != "capture_permission" || health.Capture.TargetID != "surface-1" {
		t.Fatalf("%s", got.Body)
	}
}

func TestOlderHealthObservationCannotReplaceNewerEvidence(t *testing.T) {
	s, _ := testServer(t, testOrigin)
	now := time.Now()
	s.recordInventory(now, nil)
	s.recordInventory(now.Add(-time.Second), ghostty.ErrUnresponsive)
	s.recordCapture(now, "new", nil)
	s.recordCapture(now.Add(-time.Second), "old", ghostty.ErrCapturePermission)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inventoryObservation.State != "ready" || s.captureObservation.TargetID != "new" {
		t.Fatal("stale completion replaced fresh evidence")
	}
}

func TestUnsupportedTitleActionsRejectedBeforeExecution(t *testing.T) {
	s, base := testServer(t, testOrigin)
	base.targets[0].Focused = true
	s.backend = &observingBackend{fakeBackend: base, version: "1.3.0", list: base.Targets}
	b := pairBrowser(t, s)
	raw, _ := json.Marshal(actionRequest{ID: "old-ghostty-title", Action: ghostty.Action{Kind: "rename_pane", TargetID: "surface-1", Name: "review"}})
	if got := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw)); got.State != "rejected" {
		t.Fatalf("%+v", got)
	}
	if len(base.calls) != 0 {
		t.Fatal("unsupported action reached Ghostty")
	}
}
