package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/domain"
	"github.com/gongahkia/courtsg/internal/geo"
	"github.com/gongahkia/courtsg/internal/source"
	"github.com/gongahkia/courtsg/internal/sources/sportsg"
	"github.com/gongahkia/courtsg/internal/store"
)

// Service is the only application layer used by the CLI, TUI, HTTP API and MCP.
type Service struct {
	config        config.Config
	store         *store.Store
	sources       *source.Registry
	geocoder      geo.Geocoder
	router        geo.Router
	routingDetail string
}

func Open(ctx context.Context, cfg config.Config) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	catalog := source.Catalog()
	transport := source.NewHTTPClient(catalog)
	sportSGInfo, ok := findSourceInfo(catalog, sportsg.SourceID)
	if !ok {
		return nil, fmt.Errorf("required source %q is missing from catalog", sportsg.SourceID)
	}
	sportSG, err := sportsg.New(transport, sportSGInfo)
	if err != nil {
		return nil, err
	}
	registry, err := source.NewRegistry(catalog, sportSG)
	if err != nil {
		return nil, fmt.Errorf("initialize source registry: %w", err)
	}
	for id, settings := range cfg.Sources {
		if _, ok := registry.Get(id); !ok {
			continue
		}
		if err := registry.SetEnabled(id, settings.Enabled); err != nil {
			if settings.Enabled {
				return nil, err
			}
		}
	}
	database, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	service := &Service{config: cfg, store: database, sources: registry}
	service.configureOneMap(transport)
	if err := database.UpsertSports(ctx, domain.Sports()); err != nil {
		database.Close()
		return nil, err
	}
	if err := database.UpsertSources(ctx, registry.List(), registry.Enabled); err != nil {
		database.Close()
		return nil, err
	}
	return service, nil
}

func (service *Service) configureOneMap(transport *source.HTTPClient) {
	credentials, detail := oneMapCredentials(service.config)
	if detail != "" {
		service.routingDetail = detail
		return
	}
	oneMap, err := geo.NewOneMap(transport, credentials)
	if err != nil {
		service.routingDetail = err.Error()
		return
	}
	service.geocoder = oneMap
	service.router = oneMap
	service.routingDetail = "configured"
}

func oneMapCredentials(cfg config.Config) (geo.OneMapCredentials, string) {
	resolve := func(value string) (string, error) {
		if strings.TrimSpace(value) == "" {
			return "", nil
		}
		return cfg.ResolveSecret(value)
	}
	accessToken, err := resolve(cfg.Routing.AccessToken)
	if err != nil {
		return geo.OneMapCredentials{}, "OneMap access token unavailable: " + err.Error()
	}
	if accessToken != "" {
		return geo.OneMapCredentials{AccessToken: accessToken}, ""
	}
	email, emailErr := resolve(cfg.Routing.Email)
	password, passwordErr := resolve(cfg.Routing.Password)
	if emailErr != nil || passwordErr != nil {
		detail := "OneMap credentials unavailable"
		if emailErr != nil {
			detail += ": " + emailErr.Error()
		} else {
			detail += ": " + passwordErr.Error()
		}
		return geo.OneMapCredentials{}, detail
	}
	if email == "" || password == "" {
		return geo.OneMapCredentials{}, "OneMap credentials are not configured"
	}
	return geo.OneMapCredentials{Email: email, Password: password}, ""
}

func findSourceInfo(infos []domain.SourceInfo, sourceID string) (domain.SourceInfo, bool) {
	for _, info := range infos {
		if info.ID == sourceID {
			return info, true
		}
	}
	return domain.SourceInfo{}, false
}

func (service *Service) Close() error {
	return service.store.Close()
}

func (service *Service) Sources(ctx context.Context) ([]store.SourceRecord, error) {
	return service.store.ListSources(ctx)
}

func (service *Service) Source(ctx context.Context, sourceID string) (store.SourceRecord, error) {
	return service.store.GetSource(ctx, sourceID)
}

func (service *Service) SetSourceEnabled(ctx context.Context, sourceID string, enabled bool) (store.SourceRecord, error) {
	if err := service.sources.SetEnabled(sourceID, enabled); err != nil {
		return store.SourceRecord{}, err
	}
	if err := service.store.SetSourceEnabled(ctx, sourceID, enabled); err != nil {
		return store.SourceRecord{}, err
	}
	return service.store.GetSource(ctx, sourceID)
}

func (service *Service) SourceDoctor(ctx context.Context) ([]store.SourceRecord, error) {
	records, err := service.store.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	for index, record := range records {
		if !record.Info.Policy.AllowsNetwork() || !record.Enabled {
			continue
		}
		health, err := service.sources.Health(ctx, record.Info.ID)
		if err != nil {
			return nil, err
		}
		if health.State != domain.HealthUnknown {
			if err := service.store.SaveSourceHealth(ctx, health); err != nil {
				return nil, err
			}
			record.Health = health
			records[index] = record
		}
	}
	return records, nil
}

type RefreshResult struct {
	SourceID      string `json:"source_id"`
	State         string `json:"state"`
	VenuesUpdated int    `json:"venues_updated"`
	Detail        string `json:"detail,omitempty"`
}

// Refresh fetches each selected source once. Watch evaluation is intentionally a
// separate local phase so multiple watches never multiply upstream calls.
func (service *Service) Refresh(ctx context.Context, sourceIDs []string) ([]RefreshResult, error) {
	if len(sourceIDs) == 0 {
		for _, info := range service.sources.List() {
			if service.sources.Enabled(info.ID) {
				sourceIDs = append(sourceIDs, info.ID)
			}
		}
	}
	results := make([]RefreshResult, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		info, ok := service.sources.Get(sourceID)
		if !ok {
			return results, fmt.Errorf("unknown source %q", sourceID)
		}
		if !service.sources.Enabled(sourceID) || !info.Policy.AllowsNetwork() {
			results = append(results, RefreshResult{SourceID: sourceID, State: "skipped", Detail: string(info.Policy.Status)})
			continue
		}
		adapter, err := service.sources.Adapter(sourceID)
		if err != nil {
			if errors.Is(err, source.ErrUnsupported) {
				results = append(results, RefreshResult{SourceID: sourceID, State: "skipped", Detail: "no refreshable capability"})
				continue
			}
			return results, err
		}
		result := RefreshResult{SourceID: sourceID, State: "healthy"}
		if info.Policy.Capabilities.VenueDiscovery {
			venues, err := adapter.DiscoverVenues(ctx)
			if err != nil {
				health, healthErr := adapter.Health(ctx)
				if healthErr == nil {
					_ = service.store.SaveSourceHealth(ctx, health)
				}
				result.State = "degraded"
				result.Detail = err.Error()
				results = append(results, result)
				continue
			}
			if err := service.store.UpsertVenues(ctx, venues); err != nil {
				return results, err
			}
			result.VenuesUpdated = len(venues)
		}
		health, err := adapter.Health(ctx)
		if err == nil {
			if err := service.store.SaveSourceHealth(ctx, health); err != nil {
				return results, err
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func (service *Service) Venues(ctx context.Context, filter store.VenueFilter) ([]domain.Venue, error) {
	return service.store.SearchVenues(ctx, filter)
}

func (service *Service) Venue(ctx context.Context, venueID string) (domain.Venue, error) {
	return service.store.GetVenue(ctx, venueID)
}

// Geocode searches OneMap when configured and caches the first authoritative
// result. It never needs a routing credential to be present at application start.
func (service *Service) Geocode(ctx context.Context, query string) ([]geo.Place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("geocode query cannot be empty")
	}
	key := cacheKey("geocode", strings.ToLower(query))
	if cached, ok, err := service.store.GetGeocodeCache(ctx, key, time.Now()); err != nil {
		return nil, err
	} else if ok {
		return []geo.Place{cached}, nil
	}
	if service.geocoder == nil {
		return nil, geo.ErrCredentialsRequired
	}
	places, err := service.geocoder.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(places) > 0 {
		if err := service.store.SaveGeocodeCache(ctx, key, query, places[0], time.Now().Add(30*24*time.Hour)); err != nil {
			return nil, err
		}
	}
	return places, nil
}

// Route prefers OneMap and marks the deterministic Haversine fallback so callers
// can explain the difference between route and geographic estimates.
func (service *Service) Route(ctx context.Context, origin, destination domain.Coordinates, mode geo.TravelMode) (geo.Route, error) {
	if !origin.Valid() || !destination.Valid() {
		return geo.Route{}, errors.New("route coordinates are invalid")
	}
	if !mode.Valid() {
		return geo.Route{}, fmt.Errorf("invalid travel mode %q", mode)
	}
	key := cacheKey("route", fmt.Sprintf("%.6f,%.6f:%.6f,%.6f:%s", origin.Latitude, origin.Longitude, destination.Latitude, destination.Longitude, mode))
	if cached, ok, err := service.store.GetRouteCache(ctx, key, time.Now()); err != nil {
		return geo.Route{}, err
	} else if ok && (service.router == nil || !cached.Fallback) {
		return cached, nil
	}
	if service.router == nil {
		route := geo.FallbackRoute(origin, destination, mode)
		if err := service.store.SaveRouteCache(ctx, key, route, time.Now().Add(time.Hour)); err != nil {
			return geo.Route{}, err
		}
		return route, nil
	}
	route, err := service.router.Route(ctx, origin, destination, mode)
	if err != nil {
		fallback := geo.FallbackRoute(origin, destination, mode)
		if cacheErr := service.store.SaveRouteCache(ctx, key, fallback, time.Now().Add(time.Hour)); cacheErr != nil {
			return geo.Route{}, cacheErr
		}
		return fallback, nil
	}
	if err := service.store.SaveRouteCache(ctx, key, route, time.Now().Add(6*time.Hour)); err != nil {
		return geo.Route{}, err
	}
	return route, nil
}

func cacheKey(prefix, value string) string {
	hash := sha256.Sum256([]byte(value))
	return prefix + ":" + hex.EncodeToString(hash[:])
}

func (service *Service) RoutingStatus() string {
	return service.routingDetail
}

type DoctorCheck struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type DoctorReport struct {
	Checks []DoctorCheck `json:"checks"`
}

func (service *Service) Doctor(ctx context.Context) (DoctorReport, error) {
	report := DoctorReport{}
	if err := service.store.Ping(ctx); err != nil {
		report.Checks = append(report.Checks, DoctorCheck{Name: "database", State: "error", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, DoctorCheck{Name: "database", State: "healthy", Detail: service.config.DatabasePath})
	}
	if err := service.config.Validate(); err != nil {
		report.Checks = append(report.Checks, DoctorCheck{Name: "config", State: "error", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, DoctorCheck{Name: "config", State: "healthy", Detail: "validated"})
	}
	records, err := service.SourceDoctor(ctx)
	if err != nil {
		return report, err
	}
	for _, record := range records {
		detail := string(record.Info.Policy.Status)
		if record.Health.LastError != "" {
			detail = record.Health.LastError
		}
		report.Checks = append(report.Checks, DoctorCheck{Name: "source:" + record.Info.ID, State: string(record.Health.State), Detail: detail})
	}
	return report, nil
}

func (service *Service) Config() config.Config {
	return service.config
}
