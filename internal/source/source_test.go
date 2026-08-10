package source

import (
	"context"
	"errors"
	"testing"
)

func TestCatalogPolicyGatesNetwork(t *testing.T) {
	registry, err := NewRegistry(Catalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetEnabled("safra", true); !errors.Is(err, ErrPolicyDisabled) {
		t.Fatalf("SetEnabled(safra) error = %v, want policy error", err)
	}
	health, err := registry.Health(context.Background(), "safra")
	if err != nil {
		t.Fatal(err)
	}
	if health.State != "disabled" {
		t.Fatalf("health state = %q, want disabled", health.State)
	}
}

func TestCatalogIsStableAndUnique(t *testing.T) {
	registry, err := NewRegistry(Catalog())
	if err != nil {
		t.Fatal(err)
	}
	infos := registry.List()
	if len(infos) < 10 {
		t.Fatalf("source count = %d, want broad curated corpus", len(infos))
	}
	if infos[0].ID != "kings-pickleball" {
		t.Fatalf("catalog list should be stable, got first %q", infos[0].ID)
	}
}
