package store

import (
	"context"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

func TestUpsertAndReadSource(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/kaypoh.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	info := domain.SourceInfo{ID: "test", Name: "Test", Operator: "Test", Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData, ReviewedAt: time.Now()}}
	if err := store.UpsertSources(ctx, []domain.SourceInfo{info}, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	record, err := store.GetSource(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Enabled || record.Info.Policy.Status != domain.SourceEnabledPublicData {
		t.Fatalf("unexpected source record: %#v", record)
	}
	if err := store.SetSourceEnabled(ctx, "test", false); err != nil {
		t.Fatal(err)
	}
	record, err = store.GetSource(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled || record.Health.State != domain.HealthDisabled {
		t.Fatalf("expected disabled source record, got %#v", record)
	}
}
