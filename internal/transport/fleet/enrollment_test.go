package fleettransport

import (
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestEnrollmentPlanSelectsMeshSSHAndRelay(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		adapter Adapter
		address string
		kind    fleet.EndpointKind
	}{
		{AdapterMesh, "https://100.64.0.2", fleet.EndpointMesh},
		{AdapterSSH, "onibi@host.example.test:2222", fleet.EndpointSSH},
		{AdapterRelay, "https://relay.example.test", fleet.EndpointRelay},
	} {
		plan, err := NewEnrollmentPlan(string(test.adapter), test.address, now)
		if err != nil {
			t.Fatalf("new %s plan: %v", test.adapter, err)
		}
		endpoint, err := plan.Resolve(now.Add(time.Minute))
		if err != nil {
			t.Fatalf("resolve %s plan: %v", test.adapter, err)
		}
		if endpoint.Kind != test.kind || endpoint.URL != test.address {
			t.Fatalf("endpoint = %#v", endpoint)
		}
	}
}

func TestEnrollmentPlanRejectsMalformedStaleAndIncompatibleInputs(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	valid, err := NewEnrollmentPlan("relay", "https://relay.example.test", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range []EnrollmentPlan{
		{Version: EnrollmentAdapterVersion + 1, Adapter: AdapterRelay, Address: valid.Address, IssuedAt: now, ExpiresAt: now.Add(EnrollmentPlanTTL)},
		{Version: EnrollmentAdapterVersion, Adapter: AdapterRelay, Address: valid.Address, IssuedAt: now.Add(-EnrollmentPlanTTL), ExpiresAt: now},
		{Version: EnrollmentAdapterVersion, Adapter: AdapterRelay, Address: valid.Address, IssuedAt: now.Add(time.Minute), ExpiresAt: now.Add(30 * time.Second)},
		{Version: EnrollmentAdapterVersion, Adapter: AdapterRelay, Address: "https://127.0.0.1", IssuedAt: now, ExpiresAt: now.Add(EnrollmentPlanTTL)},
		{Version: EnrollmentAdapterVersion, Adapter: AdapterMesh, Address: "https://localhost", IssuedAt: now, ExpiresAt: now.Add(EnrollmentPlanTTL)},
		{Version: EnrollmentAdapterVersion, Adapter: AdapterSSH, Address: "onibi@host.example.test:not-a-port", IssuedAt: now, ExpiresAt: now.Add(EnrollmentPlanTTL)},
	} {
		if _, err := plan.Resolve(now); err == nil {
			t.Fatalf("expected invalid plan: %#v", plan)
		}
	}
}

func TestEnrollmentPlanBindsOnlyEligibleHost(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	plan, err := NewEnrollmentPlan("relay", "https://relay.example.test", now)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{
		ID:              "host-relay",
		OwnerID:         "owner-local",
		DisplayName:     "Relay host",
		IdentityPublic:  "identity-public",
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://100.64.0.2"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		State:           fleet.HostStatePending,
		RegisteredAt:    now,
	}
	selected, err := plan.ApplyToHost(host, now)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Endpoint.Kind != fleet.EndpointRelay || selected.Endpoint.URL != "https://relay.example.test" {
		t.Fatalf("selected endpoint = %#v", selected.Endpoint)
	}
	revokedAt := now
	host.State = fleet.HostStateRevoked
	host.RevokedAt = &revokedAt
	if _, err := plan.ApplyToHost(host, now); err == nil {
		t.Fatal("expected revoked host error")
	}
}

func TestEnrollmentPlanResolvesConcurrently(t *testing.T) {
	now := time.Now().UTC()
	plan, err := NewEnrollmentPlan("mesh", "https://100.64.0.2", now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range cap(errs) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := plan.Resolve(now)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
