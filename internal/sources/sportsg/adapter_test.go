package sportsg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseGeoJSONFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "facilities.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	venues, err := ParseGeoJSON(body, time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(venues) != 2 {
		t.Fatalf("venue count = %d, want 2", len(venues))
	}
	if venues[0].ID != "sportsg-facilities:venue:340" || venues[0].Address != "10 West Coast Walk" {
		t.Fatalf("unexpected first venue: %#v", venues[0])
	}
	if venues[1].Coordinates.Latitude != 1.3209776742306738 {
		t.Fatalf("unexpected coordinate: %#v", venues[1].Coordinates)
	}
}

func TestParseGeoJSONRejectsEmptyFeatureCollection(t *testing.T) {
	if _, err := ParseGeoJSON([]byte(`{"type":"FeatureCollection","features":[]}`), time.Now()); err == nil {
		t.Fatal("expected empty collection error")
	}
}
