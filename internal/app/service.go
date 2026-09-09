package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/kaypoh/internal/browser"
	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/geo"
	"github.com/gongahkia/kaypoh/internal/notifier"
	"github.com/gongahkia/kaypoh/internal/query"
	"github.com/gongahkia/kaypoh/internal/ranking"
	"github.com/gongahkia/kaypoh/internal/source"
	"github.com/gongahkia/kaypoh/internal/sources/activesg"
	"github.com/gongahkia/kaypoh/internal/sources/fallback"
	"github.com/gongahkia/kaypoh/internal/sources/onepa"
	"github.com/gongahkia/kaypoh/internal/sources/partner"
	"github.com/gongahkia/kaypoh/internal/sources/perfectgym"
	"github.com/gongahkia/kaypoh/internal/sources/publicavailability"
	"github.com/gongahkia/kaypoh/internal/sources/sportsg"
	"github.com/gongahkia/kaypoh/internal/store"
)

// Service is the only application layer used by the CLI, TUI, HTTP API and MCP.
type Service struct {
	config        config.Config
	store         *store.Store
	sources       *source.Registry
	browser       browser.Fetcher
	geocoder      geo.Geocoder
	router        geo.Router
	notifier      notifier.Sender
	routingDetail string
}

func Open(ctx context.Context, cfg config.Config) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	catalog := source.Catalog()
	transport := source.NewHTTPClient(catalog)
	browserClient := browser.New(2)
	sportSGInfo, ok := findSourceInfo(catalog, sportsg.SourceID)
	if !ok {
		return nil, fmt.Errorf("required source %q is missing from catalog", sportsg.SourceID)
	}
	sportSG, err := sportsg.New(transport, sportSGInfo)
	if err != nil {
		_ = browserClient.Close()
		return nil, err
	}
	adapters := []source.Adapter{sportSG}
	for _, spec := range partnerSpecs() {
		info, ok := findSourceInfo(catalog, spec.ID)
		if !ok {
			_ = browserClient.Close()
			return nil, fmt.Errorf("required source %q is missing from catalog", spec.ID)
		}
		settings, err := resolveSourceSettings(cfg, spec.ID)
		if err != nil {
			_ = browserClient.Close()
			return nil, err
		}
		var adapter source.Adapter
		if spec.ID == activesg.SourceID && settings.ActiveSG.Enabled {
			adapter, err = newActiveSGAdapter(info, spec, settings, transport, browserClient)
		} else if spec.ID == onepa.SourceID && settings.OnePA.Enabled {
			adapter, err = newOnePAAdapter(info, spec, settings, transport, browserClient)
		} else if spec.ID == perfectgym.SourceID && settings.PerfectGym.Enabled {
			adapter, err = newPerfectGymAdapter(info, spec, settings, transport, browserClient)
		} else if publicavailability.Supports(spec.ID) && !configuredPartnerAccess(settings) {
			adapter, err = publicavailability.New(info, settings, transport)
		} else {
			adapter, err = partner.New(info, spec, settings, transport, browserClient)
		}
		if err != nil {
			_ = browserClient.Close()
			return nil, err
		}
		adapters = append(adapters, adapter)
	}
	registry, err := source.NewRegistry(catalog, adapters...)
	if err != nil {
		_ = browserClient.Close()
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
		_ = browserClient.Close()
		return nil, err
	}
	service := &Service{config: cfg, store: database, sources: registry, browser: browserClient, notifier: notifier.New(cfg)}
	service.configureOneMap(transport)
	if err := database.UpsertSources(ctx, registry.List(), registry.Enabled); err != nil {
		database.Close()
		_ = browserClient.Close()
		return nil, err
	}
	return service, nil
}

// newOnePAAdapter keeps generic partner modes and the dedicated anonymous
// onePA reader as independent attempts. Generic API, browser, and public
// access run first; the direct availability reader is the final fallback.
func newOnePAAdapter(info domain.SourceInfo, spec partner.Spec, settings config.Source, transport *source.HTTPClient, browserClient *browser.Playwright) (source.Adapter, error) {
	dedicated, err := onepa.New(info, settings, transport)
	if err != nil {
		return nil, err
	}
	if !configuredPartnerAccess(settings) {
		return dedicated, nil
	}
	generic, err := partner.New(info, spec, settings, transport, browserClient)
	if err != nil {
		return nil, err
	}
	attempts := make([]fallback.Attempt, 0, len(generic.Modes())+1)
	for _, mode := range generic.Modes() {
		mode := mode
		attempts = append(attempts, fallback.Attempt{Mode: mode, Fetch: func(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			return generic.FetchMode(ctx, request, mode)
		}})
	}
	attempts = append(attempts, fallback.Attempt{Mode: onepa.AccessMode, Fetch: dedicated.FetchSnapshot})
	return fallback.New(info, attempts)
}

// newActiveSGAdapter keeps the generic partner modes and the dedicated
// date-card scanner as independent attempts. Generic API, browser, and public
// access are attempted first; the dedicated reader is the final fallback.
func newActiveSGAdapter(info domain.SourceInfo, spec partner.Spec, settings config.Source, transport *source.HTTPClient, browserClient *browser.Playwright) (source.Adapter, error) {
	dedicated, err := activesg.New(info, settings, browserClient)
	if err != nil {
		return nil, err
	}
	if !configuredPartnerAccess(settings) {
		return dedicated, nil
	}
	generic, err := partner.New(info, spec, settings, transport, browserClient)
	if err != nil {
		return nil, err
	}
	attempts := make([]fallback.Attempt, 0, len(generic.Modes())+1)
	for _, mode := range generic.Modes() {
		mode := mode
		attempts = append(attempts, fallback.Attempt{Mode: mode, Fetch: func(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			return generic.FetchMode(ctx, request, mode)
		}})
	}
	attempts = append(attempts, fallback.Attempt{Mode: activesg.AccessMode, Fetch: dedicated.FetchSnapshot})
	return fallback.New(info, attempts)
}

// newPerfectGymAdapter keeps generic partner modes and the dedicated Kallang
// calendar reader as independent attempts. Generic API, browser, and public
// access run first; the calendar reader is the final fallback.
func newPerfectGymAdapter(info domain.SourceInfo, spec partner.Spec, settings config.Source, transport *source.HTTPClient, browserClient *browser.Playwright) (source.Adapter, error) {
	dedicated, err := perfectgym.New(info, settings, browserClient)
	if err != nil {
		return nil, err
	}
	if !configuredPartnerAccess(settings) {
		return dedicated, nil
	}
	generic, err := partner.New(info, spec, settings, transport, browserClient)
	if err != nil {
		return nil, err
	}
	attempts := make([]fallback.Attempt, 0, len(generic.Modes())+1)
	for _, mode := range generic.Modes() {
		mode := mode
		attempts = append(attempts, fallback.Attempt{Mode: mode, Fetch: func(ctx context.Context, request source.AvailabilityRequest) (source.AvailabilitySnapshot, error) {
			return generic.FetchMode(ctx, request, mode)
		}})
	}
	attempts = append(attempts, fallback.Attempt{Mode: perfectgym.AccessMode, Fetch: dedicated.FetchSnapshot})
	return fallback.New(info, attempts)
}

// configuredPartnerAccess gives an operator-supplied API, browser reader, or
// generic public mapper precedence over a built-in anonymous reader. This lets
// a provider migrate to an official API without removing the safe fallback.
func configuredPartnerAccess(settings config.Source) bool {
	return settings.API.Enabled || settings.Browser.Enabled || settings.Public.Enabled
}

func partnerSpecs() []partner.Spec {
	return []partner.Spec{
		{ID: "myactivesg"},
		{ID: "onepa"},
		{ID: "the-kallang"},
		{ID: "sba-stadium"},
		{ID: "singapore-badminton-hall"},
		{ID: "smash-arena"},
		{ID: "wyse-active"},
	}
}

func resolveSourceSettings(cfg config.Config, sourceID string) (config.Source, error) {
	settings := cfg.Sources[sourceID]
	if !settings.Enabled {
		return settings, nil
	}
	resolve := func(label, value string) (string, error) {
		if strings.TrimSpace(value) == "" {
			return "", nil
		}
		resolved, err := cfg.ResolveSecret(value)
		if err != nil {
			return "", fmt.Errorf("resolve sources.%s.%s: %w", sourceID, label, err)
		}
		return resolved, nil
	}
	var err error
	if settings.API.BearerToken, err = resolve("api.bearer_token", settings.API.BearerToken); err != nil {
		return config.Source{}, err
	}
	if settings.Browser.Username, err = resolve("browser.username", settings.Browser.Username); err != nil {
		return config.Source{}, err
	}
	if settings.Browser.Password, err = resolve("browser.password", settings.Browser.Password); err != nil {
		return config.Source{}, err
	}
	if settings.Browser.SessionStateBase64, err = resolve("browser.session_state_base64", settings.Browser.SessionStateBase64); err != nil {
		return config.Source{}, err
	}
	if settings.ActiveSG.SessionStateBase64, err = resolve("activesg.session_state_base64", settings.ActiveSG.SessionStateBase64); err != nil {
		return config.Source{}, err
	}
	if settings.PerfectGym.SessionStateBase64, err = resolve("perfectgym.session_state_base64", settings.PerfectGym.SessionStateBase64); err != nil {
		return config.Source{}, err
	}
	return settings, nil
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
	var result error
	if service.browser != nil {
		result = service.browser.Close()
	}
	if err := service.store.Close(); err != nil && result == nil {
		result = err
	}
	return result
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
	SlotsUpdated  int    `json:"slots_updated"`
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
		record, err := service.store.GetSource(ctx, sourceID)
		if err != nil {
			return results, err
		}
		if record.Health.LastSuccess != nil && service.refreshInterval(sourceID, info) > 0 {
			nextAllowed := record.Health.LastSuccess.Add(service.refreshInterval(sourceID, info))
			if time.Now().Before(nextAllowed) {
				results = append(results, RefreshResult{SourceID: sourceID, State: "skipped", Detail: "poll floor until " + nextAllowed.UTC().Format(time.RFC3339)})
				continue
			}
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
		if info.Policy.Capabilities.Availability {
			if snapshotAdapter, ok := adapter.(source.SnapshotAdapter); ok {
				request := service.availabilityRequest(sourceID, info)
				snapshot, err := snapshotAdapter.FetchSnapshot(ctx, request)
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
				if err := service.store.UpsertVenues(ctx, snapshot.Venues); err != nil {
					return results, err
				}
				if err := service.store.UpsertAvailability(ctx, snapshot.Slots); err != nil {
					return results, err
				}
				if err := service.store.ReconcileAvailability(ctx, sourceID, request.StartDate, request.EndDate, snapshot.Slots, time.Now().UTC()); err != nil {
					return results, err
				}
				if err := service.store.RecordAvailabilityObservation(ctx, sourceID, request.StartDate, request.EndDate, snapshot.Slots, time.Now().UTC()); err != nil {
					return results, err
				}
				result.VenuesUpdated += len(snapshot.Venues)
				result.SlotsUpdated = len(snapshot.Slots)
			}
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

func (service *Service) refreshInterval(sourceID string, info domain.SourceInfo) time.Duration {
	minutes := service.config.Daemon.RefreshMinutes
	if settings, ok := service.config.Sources[sourceID]; ok && settings.RefreshMinutes > 0 {
		minutes = settings.RefreshMinutes
	}
	configured := time.Duration(minutes) * time.Minute
	if configured < info.Policy.PollFloor {
		return info.Policy.PollFloor
	}
	return configured
}

func (service *Service) availabilityRequest(sourceID string, info domain.SourceInfo) source.AvailabilityRequest {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	now := time.Now().In(location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).UTC()
	days := info.Policy.AvailabilityMaxDays
	if settings, ok := service.config.Sources[sourceID]; ok && settings.AvailabilityMaxDays > 0 {
		days = settings.AvailabilityMaxDays
	}
	if days < 1 {
		days = 14
	}
	return source.AvailabilityRequest{StartDate: start, EndDate: start.AddDate(0, 0, days)}
}

func (service *Service) Venues(ctx context.Context, filter store.VenueFilter) ([]domain.Venue, error) {
	return service.store.SearchVenues(ctx, filter)
}

func (service *Service) Venue(ctx context.Context, venueID string) (domain.Venue, error) {
	return service.store.GetVenue(ctx, venueID)
}

// ImportManualAvailability persists user-supplied availability without making
// any network request. The local-manual source is deliberately separate from
// venue provenance, so official discovery data remains distinguishable.
func (service *Service) ImportManualAvailability(ctx context.Context, slots []domain.AvailabilitySlot) ([]domain.AvailabilitySlot, error) {
	now := time.Now().UTC()
	for index := range slots {
		slot := &slots[index]
		if slot.SourceID == "" {
			slot.SourceID = "local-manual"
		}
		if slot.SourceID != "local-manual" {
			return nil, fmt.Errorf("manual availability slot %q must use source local-manual", slot.ID)
		}
		if slot.ID == "" {
			slot.ID = manualSlotID(*slot)
		}
		if slot.Status == "" {
			slot.Status = domain.AvailabilityAvailable
		}
		if slot.Currency == "" {
			slot.Currency = "SGD"
		}
		if slot.ObservedAt.IsZero() {
			slot.ObservedAt = now
		}
		if slot.FetchedAt.IsZero() {
			slot.FetchedAt = now
		}
		if slot.StaleAfter.IsZero() {
			slot.StaleAfter = now.Add(24 * time.Hour)
		}
		if slot.Provenance.SourceID == "" {
			slot.Provenance.SourceID = "local-manual"
		}
		if slot.Provenance.ObservedAt.IsZero() {
			slot.Provenance.ObservedAt = slot.ObservedAt
		}
		if slot.Provenance.FetchedAt.IsZero() {
			slot.Provenance.FetchedAt = slot.FetchedAt
		}
	}
	if err := service.store.UpsertAvailability(ctx, slots); err != nil {
		return nil, err
	}
	return slots, nil
}

// Search runs entirely against normalized local state. It never triggers a
// network refresh, which lets watches, CLI, MCP, and the TUI share one safe path.
func (service *Service) Search(ctx context.Context, input domain.Query) ([]domain.SearchResult, error) {
	criteria, err := query.Normalize(input)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from, until := availabilityBounds(criteria, now)
	rows, err := service.store.ListAvailability(ctx, from, until, 2_000)
	if err != nil {
		return nil, err
	}
	candidates, err := query.Filter(rows, criteria, now)
	if err != nil {
		return nil, err
	}
	return ranking.Rank(ctx, candidates, criteria, service, now)
}

func availabilityBounds(criteria domain.Query, now time.Time) (time.Time, time.Time) {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		location = time.FixedZone("SGT", 8*60*60)
	}
	from := now
	if criteria.StartDate != nil {
		date := criteria.StartDate.In(location)
		from = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location).UTC()
		if from.Before(now) {
			from = now
		}
	}
	until := from.AddDate(0, 0, 30)
	if criteria.EndDate != nil {
		date := criteria.EndDate.In(location)
		until = time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, location).UTC()
	}
	return from, until
}

func manualSlotID(slot domain.AvailabilitySlot) string {
	value := strings.Join([]string{slot.VenueID, slot.FacilityID, slot.CourtName, slot.Start.UTC().Format(time.RFC3339Nano), slot.End.UTC().Format(time.RFC3339Nano)}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "manual:" + hex.EncodeToString(digest[:12])
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
