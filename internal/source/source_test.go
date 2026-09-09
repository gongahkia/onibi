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
	if err := registry.SetEnabled("local-manual", true); !errors.Is(err, ErrPolicyDisabled) {
		t.Fatalf("SetEnabled(local-manual) error = %v, want policy error", err)
	}
	health, err := registry.Health(context.Background(), "local-manual")
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
	if len(infos) != 10 {
		t.Fatalf("source count = %d, want 10 configured sources", len(infos))
	}
	if infos[0].ID != "local-manual" {
		t.Fatalf("catalog list should be stable, got first %q", infos[0].ID)
	}
}
