package sportsg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

const (
	SourceID        = "sportsg-facilities"
	datasetEndpoint = "https://api-open.data.gov.sg/v1/public/api/datasets/d_9b87bab59d036a60fad2a91530e10773/poll-download"
)

type Adapter struct {
	client *source.HTTPClient
	info   domain.SourceInfo

	mu     sync.RWMutex
	health domain.SourceHealth
}

func New(client *source.HTTPClient, info domain.SourceInfo) (*Adapter, error) {
	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	if info.ID != SourceID {
		return nil, fmt.Errorf("expected source ID %q, got %q", SourceID, info.ID)
	}
	return &Adapter{client: client, info: info, health: domain.SourceHealth{SourceID: SourceID, State: domain.HealthUnknown}}, nil
}

func (adapter *Adapter) Info() domain.SourceInfo { return adapter.info }

func (adapter *Adapter) DiscoverVenues(ctx context.Context) ([]domain.Venue, error) {
	started := time.Now()
	poll, err := adapter.client.Fetch(ctx, source.HTTPRequest{SourceID: SourceID, URL: datasetEndpoint, CacheTTL: 5 * time.Minute})
	if err != nil {
		adapter.setFailure(started, err)
		return nil, fmt.Errorf("request SportSG dataset download: %w", err)
	}
	var download struct {
		Code int `json:"code"`
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
		ErrorMessage string `json:"errorMsg"`
	}
	if err := json.Unmarshal(poll.Body, &download); err != nil {
		adapter.setFailure(started, err)
		return nil, fmt.Errorf("decode SportSG dataset response: %w", err)
	}
	if download.Code != 0 || download.Data.URL == "" {
		err := fmt.Errorf("dataset API returned code %d: %s", download.Code, download.ErrorMessage)
		adapter.setFailure(started, err)
		return nil, err
	}
	if _, err := url.ParseRequestURI(download.Data.URL); err != nil {
		adapter.setFailure(started, err)
		return nil, fmt.Errorf("invalid SportSG download URL: %w", err)
	}
	data, err := adapter.client.Fetch(ctx, source.HTTPRequest{SourceID: SourceID, URL: download.Data.URL, CacheTTL: 24 * time.Hour})
	if err != nil {
		adapter.setFailure(started, err)
		return nil, fmt.Errorf("download SportSG GeoJSON: %w", err)
	}
	venues, err := ParseGeoJSON(data.Body, time.Now())
	if err != nil {
		adapter.setFailure(started, err)
		return nil, err
	}
	adapter.mu.Lock()
	now := time.Now()
	adapter.health = domain.SourceHealth{SourceID: SourceID, State: domain.HealthHealthy, LastAttempt: &now, LastSuccess: &now, LastCategory: "success", LatencyMilliseconds: time.Since(started).Milliseconds(), RecordsParsed: len(venues)}
	adapter.mu.Unlock()
	return venues, nil
}

func (adapter *Adapter) FetchAvailability(context.Context, source.AvailabilityRequest) ([]domain.AvailabilitySlot, error) {
	return nil, source.ErrUnsupported
}

func (adapter *Adapter) Health(context.Context) (domain.SourceHealth, error) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	return adapter.health, nil
}

func (adapter *Adapter) setFailure(started time.Time, err error) {
	category := "adapter"
	var httpErr *source.HTTPError
	if errors.As(err, &httpErr) {
		category = httpErr.Category
	}
	adapter.mu.Lock()
	now := time.Now()
	previousFailures := adapter.health.ConsecutiveFailures
	adapter.health = domain.SourceHealth{SourceID: SourceID, State: domain.HealthDegraded, LastAttempt: &now, LastCategory: category, LatencyMilliseconds: time.Since(started).Milliseconds(), ConsecutiveFailures: previousFailures + 1, LastError: err.Error()}
	adapter.mu.Unlock()
}

func ParseGeoJSON(body []byte, fetchedAt time.Time) ([]domain.Venue, error) {
	var document geoJSON
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("decode SportSG GeoJSON: %w", err)
	}
	if document.Type != "FeatureCollection" {
		return nil, fmt.Errorf("unexpected GeoJSON type %q", document.Type)
	}
	if len(document.Features) == 0 {
		return nil, errors.New("SportSG GeoJSON contains no features")
	}
	venues := make([]domain.Venue, 0, len(document.Features))
	seen := make(map[string]struct{}, len(document.Features))
	for index, feature := range document.Features {
		venue, err := normalizeFeature(feature, fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("normalize feature %d: %w", index, err)
		}
		if _, exists := seen[venue.ID]; exists {
			return nil, fmt.Errorf("duplicate feature ID %q", venue.ID)
		}
		seen[venue.ID] = struct{}{}
		venues = append(venues, venue)
	}
	return venues, nil
}

type geoJSON struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type feature struct {
	Type       string         `json:"type"`
	Geometry   geometry       `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"`
}

func normalizeFeature(feature feature, fetchedAt time.Time) (domain.Venue, error) {
	if feature.Type != "Feature" || feature.Geometry.Type != "Point" || len(feature.Geometry.Coordinates) != 2 {
		return domain.Venue{}, errors.New("expected Point feature with two coordinates")
	}
	objectID := propertyString(feature.Properties, "OBJECTID")
	name := strings.TrimSpace(propertyString(feature.Properties, "VENUE"))
	if objectID == "" || name == "" {
		return domain.Venue{}, errors.New("OBJECTID and VENUE are required")
	}
	coordinates := domain.Coordinates{Longitude: feature.Geometry.Coordinates[0], Latitude: feature.Geometry.Coordinates[1]}
	if !coordinates.Valid() {
		return domain.Venue{}, fmt.Errorf("invalid coordinates for %q", objectID)
	}
	address := strings.Join(nonEmpty(
		strings.TrimSpace(propertyString(feature.Properties, "ADDRESSBLOCKHOUSENUMBER")),
		strings.TrimSpace(propertyString(feature.Properties, "ADDRESSSTREETNAME")),
	), " ")
	bookingURL := strings.TrimSpace(propertyString(feature.Properties, "DETAILS"))
	bookingURLs := []string{}
	if bookingURL != "" {
		parsed, err := url.ParseRequestURI(bookingURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return domain.Venue{}, fmt.Errorf("invalid DETAILS URL for %q", objectID)
		}
		bookingURLs = append(bookingURLs, bookingURL)
	}
	evidence, err := json.Marshal(feature)
	if err != nil {
		return domain.Venue{}, fmt.Errorf("encode feature evidence: %w", err)
	}
	hash := sha256.Sum256(evidence)
	return domain.Venue{
		ID:          "sportsg-facilities:venue:" + objectID,
		SourceIDs:   []string{objectID},
		Name:        name,
		Address:     address,
		PostalCode:  strings.TrimSpace(propertyString(feature.Properties, "POSTAL_CODE")),
		Coordinates: coordinates,
		BookingURLs: bookingURLs,
		Provenance: domain.Provenance{
			SourceID:        SourceID,
			SourceReference: objectID,
			ObservedAt:      fetchedAt,
			FetchedAt:       fetchedAt,
			AdapterVersion:  "v1",
			EvidenceHash:    hex.EncodeToString(hash[:]),
			Confidence:      1,
		},
	}, nil
}

func propertyString(properties map[string]any, key string) string {
	value, ok := properties[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
