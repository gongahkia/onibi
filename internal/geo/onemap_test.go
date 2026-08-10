package geo

import (
	"errors"
	"testing"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

func TestParseSearchResponse(t *testing.T) {
	places, err := parseSearchResponse([]byte(`{"found":1,"results":[{"SEARCHVAL":"640 ROWELL ROAD SINGAPORE 200640","ADDRESS":"640 ROWELL ROAD SINGAPORE 200640","POSTAL":"200640","LATITUDE":"1.30743547948389","LONGITUDE":"103.854713903431"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 || places[0].Coordinates.Latitude != 1.30743547948389 {
		t.Fatalf("places = %#v", places)
	}
}

func TestParseSearchResponseRejectsAPIError(t *testing.T) {
	if _, err := parseSearchResponse([]byte(`{"error":"Authentication token missing."}`)); err == nil {
		t.Fatal("expected API error")
	}
}

func TestParseRouteResponse(t *testing.T) {
	origin := domain.Coordinates{Latitude: 1.3, Longitude: 103.8}
	destination := domain.Coordinates{Latitude: 1.31, Longitude: 103.81}
	route, err := parseRouteResponse([]byte(`{"status":0,"status_message":"Found","route_summary":{"total_time":12,"total_distance":5000}}`), origin, destination, ModePublicTransport)
	if err != nil {
		t.Fatal(err)
	}
	if route.Duration.Minutes() != 12 || route.DistanceMeters != 5000 || route.Fallback {
		t.Fatalf("route = %#v", route)
	}
}

func TestNewOneMapRequiresCompleteCredentials(t *testing.T) {
	client := source.NewHTTPClient([]domain.SourceInfo{
		{
			ID:       "onemap",
			Name:     "OneMap",
			Operator: "SLA",
			Policy: domain.SourcePolicy{
				Status:         domain.SourceEnabledOfficialAPI,
				PermittedHosts: []string{"www.onemap.gov.sg"},
			},
		},
	})
	if _, err := NewOneMap(client, OneMapCredentials{}); !errors.Is(err, ErrCredentialsRequired) {
		t.Fatalf("expected credentials error, got %v", err)
	}
}
