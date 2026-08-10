package source

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

var (
	ErrPolicyDisabled = errors.New("source network access is disabled by policy")
	ErrUnsupported    = errors.New("source does not support this capability")
)

type AvailabilityRequest struct {
	Sports    []string
	VenueIDs  []string
	StartDate time.Time
	EndDate   time.Time
}

type Adapter interface {
	Info() domain.SourceInfo
	DiscoverVenues(context.Context) ([]domain.Venue, error)
	FetchAvailability(context.Context, AvailabilityRequest) ([]domain.AvailabilitySlot, error)
	Health(context.Context) (domain.SourceHealth, error)
}

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	infos    map[string]domain.SourceInfo
	enabled  map[string]bool
}

func NewRegistry(infos []domain.SourceInfo, adapters ...Adapter) (*Registry, error) {
	registry := &Registry{
		adapters: make(map[string]Adapter),
		infos:    make(map[string]domain.SourceInfo),
		enabled:  make(map[string]bool),
	}
	for _, info := range infos {
		if err := registry.addInfo(info); err != nil {
			return nil, err
		}
	}
	for _, adapter := range adapters {
		info := adapter.Info()
		if _, ok := registry.infos[info.ID]; !ok {
			if err := registry.addInfo(info); err != nil {
				return nil, err
			}
		}
		if _, exists := registry.adapters[info.ID]; exists {
			return nil, fmt.Errorf("duplicate source adapter %q", info.ID)
		}
		registry.adapters[info.ID] = adapter
	}
	return registry, nil
}

func (registry *Registry) addInfo(info domain.SourceInfo) error {
	if info.ID == "" || info.Name == "" || info.Operator == "" {
		return errors.New("source id, name, and operator are required")
	}
	if _, exists := registry.infos[info.ID]; exists {
		return fmt.Errorf("duplicate source %q", info.ID)
	}
	registry.infos[info.ID] = info
	registry.enabled[info.ID] = info.Policy.AllowsNetwork()
	return nil
}

func (registry *Registry) List() []domain.SourceInfo {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	infos := make([]domain.SourceInfo, 0, len(registry.infos))
	for _, info := range registry.infos {
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

func (registry *Registry) Get(id string) (domain.SourceInfo, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	info, ok := registry.infos[id]
	return info, ok
}

func (registry *Registry) Enabled(id string) bool {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.enabled[id]
}

func (registry *Registry) SetEnabled(id string, enabled bool) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	info, ok := registry.infos[id]
	if !ok {
		return fmt.Errorf("unknown source %q", id)
	}
	if enabled && !info.Policy.AllowsNetwork() {
		return fmt.Errorf("cannot enable %q: %w (%s)", id, ErrPolicyDisabled, info.Policy.Status)
	}
	registry.enabled[id] = enabled
	return nil
}

func (registry *Registry) Adapter(id string) (Adapter, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	info, ok := registry.infos[id]
	if !ok {
		return nil, fmt.Errorf("unknown source %q", id)
	}
	if !registry.enabled[id] || !info.Policy.AllowsNetwork() {
		return nil, fmt.Errorf("%s: %w", id, ErrPolicyDisabled)
	}
	adapter, ok := registry.adapters[id]
	if !ok {
		return nil, fmt.Errorf("%s: %w", id, ErrUnsupported)
	}
	return adapter, nil
}

func (registry *Registry) Health(ctx context.Context, id string) (domain.SourceHealth, error) {
	info, ok := registry.Get(id)
	if !ok {
		return domain.SourceHealth{}, fmt.Errorf("unknown source %q", id)
	}
	if !info.Policy.AllowsNetwork() {
		return domain.SourceHealth{SourceID: id, State: domain.HealthDisabled, LastCategory: string(info.Policy.Status)}, nil
	}
	if !registry.Enabled(id) {
		return domain.SourceHealth{SourceID: id, State: domain.HealthDisabled, LastCategory: "disabled_by_config"}, nil
	}
	adapter, err := registry.Adapter(id)
	if errors.Is(err, ErrUnsupported) {
		return domain.SourceHealth{SourceID: id, State: domain.HealthUnknown, LastCategory: "adapter_not_configured"}, nil
	}
	if err != nil {
		return domain.SourceHealth{}, err
	}
	return adapter.Health(ctx)
}
