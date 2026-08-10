package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/source"
)

func TestOpenSeedsSourcePolicy(t *testing.T) {
	config, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	config.DatabasePath = filepath.Join(t.TempDir(), "courtsg.db")
	service, err := Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	record, err := service.Source(context.Background(), "safra")
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled {
		t.Fatal("SAFRA must not be enabled")
	}
	if _, err := service.SetSourceEnabled(context.Background(), "safra", true); !errors.Is(err, source.ErrPolicyDisabled) {
		t.Fatalf("SetSourceEnabled(safra) = %v, want policy error", err)
	}
}
