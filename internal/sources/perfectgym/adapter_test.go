package perfectgym

import (
	"context"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/browser"
	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

type scannerStub struct {
	entries []browser.PerfectGymAvailability
	request browser.PerfectGymScanRequest
	err     error
}

func (stub *scannerStub) ScanPerfectGym(_ context.Context, request browser.PerfectGymScanRequest) ([]browser.PerfectGymAvailability, error) {
	stub.request = request
	return stub.entries, stub.err
}

func TestAdapterNormalizesBookableVenueLevelSlots(t *testing.T) {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.September, 1, 7, 0, 0, 0, location)
	stub := &scannerStub{entries: []browser.PerfectGymAvailability{
		{Start: start, End: start.Add(time.Hour), Status: "Bookable"},
		{Start: start, End: start.Add(time.Hour), Status: "Bookable"},
		{Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), Status: "Unavailable"},
	}}
	adapter := testAdapter(t, stub)
	snapshot, err := adapter.FetchSnapshot(context.Background(), source.AvailabilityRequest{StartDate: start.UTC(), EndDate: start.AddDate(0, 0, 1).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Venues) != 1 || len(snapshot.Slots) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	slot := snapshot.Slots[0]
	if slot.CourtName != "" || slot.Status != domain.AvailabilityAvailable || slot.End.Sub(slot.Start) != time.Hour {
		t.Fatalf("slot = %#v", slot)
	}
	if stub.request.FacilityTypeName != defaultFacilityType || stub.request.EndDate.IsZero() {
		t.Fatalf("scan request = %#v", stub.request)
	}
}

func TestAdapterRejectsOtherPerfectGymHost(t *testing.T) {
	settings := config.Source{Enabled: true, PerfectGym: config.SourcePerfectGym{Enabled: true, AvailabilityURL: "https://example.com/clientportal2/", SessionStateBase64: "state"}}
	if _, err := New(testInfo(), settings, &scannerStub{}); err == nil {
		t.Fatal("unapproved PerfectGym host was accepted")
	}
}

func TestAdapterRejectsNonBadmintonFacilityType(t *testing.T) {
	settings := config.Source{Enabled: true, PerfectGym: config.SourcePerfectGym{Enabled: true, AvailabilityURL: defaultAvailabilityURL, FacilityTypeName: "Tennis Indoor Courts", SessionStateBase64: "state"}}
	if _, err := New(testInfo(), settings, &scannerStub{}); err == nil {
		t.Fatal("non-badminton facility type was accepted")
	}
}

func testAdapter(t *testing.T, scanner browser.PerfectGymScanner) *Adapter {
	t.Helper()
	settings := config.Source{Enabled: true, RefreshMinutes: 60, PerfectGym: config.SourcePerfectGym{Enabled: true, AvailabilityURL: defaultAvailabilityURL, SessionStateBase64: "state"}}
	adapter, err := New(testInfo(), settings, scanner)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testInfo() domain.SourceInfo {
	return domain.SourceInfo{ID: SourceID, Name: "The Kallang / OCBC Arena", Operator: "The Kallang Group", Policy: domain.SourcePolicy{PermittedHosts: []string{"thekallang.perfectgym.com"}, Timeout: time.Second}}
}
