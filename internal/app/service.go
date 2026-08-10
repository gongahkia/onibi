package app

import (
	"context"
	"fmt"

	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/domain"
	"github.com/gongahkia/courtsg/internal/source"
	"github.com/gongahkia/courtsg/internal/store"
)

// Service is the only application layer used by the CLI, TUI, HTTP API and MCP.
type Service struct {
	config  config.Config
	store   *store.Store
	sources *source.Registry
}

func Open(ctx context.Context, cfg config.Config) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	registry, err := source.NewRegistry(source.Catalog())
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
