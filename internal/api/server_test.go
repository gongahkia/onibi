package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/kaypoh/internal/app"
	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
)

func TestSearchEndpointUsesApplicationService(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	service, err := app.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	now := time.Now().UTC()
	if _, err := service.ImportManualAvailability(context.Background(), []domain.AvailabilitySlot{{SportID: "tennis", VenueID: "missing", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)}}); err == nil {
		t.Fatal("manual import unexpectedly accepted a missing venue")
	}
	server, err := New(service, cfg)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewBufferString(`{"sports":["badminton"],"minimum_duration":3600000000000}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result []domain.SearchResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("search response should encode an empty array")
	}
}

func TestRemoteAPIRequiresBearerToken(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabasePath = filepath.Join(t.TempDir(), "kaypoh.db")
	cfg.API = config.API{Address: "0.0.0.0:8373", AllowRemote: true, AuthToken: "test-token"}
	service, err := app.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	server, err := New(service, cfg)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.Code)
	}
}
