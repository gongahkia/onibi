// Package mcpserver exposes the local service through the official MCP Go SDK.
package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gongahkia/courtsg/internal/app"
	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/domain"
	"github.com/gongahkia/courtsg/internal/store"
)

type Server struct{ server *mcp.Server }

type SearchInput struct {
	Sports                 []string `json:"sports,omitempty" jsonschema:"canonical sport IDs or aliases"`
	Sources                []string `json:"sources,omitempty" jsonschema:"optional source IDs"`
	VenueIDs               []string `json:"venue_ids,omitempty" jsonschema:"optional venue IDs"`
	Date                   string   `json:"date,omitempty" jsonschema:"optional local date YYYY-MM-DD"`
	MinimumDurationMinutes int      `json:"minimum_duration_minutes,omitempty" jsonschema:"minimum contiguous interval in minutes; defaults to 60"`
	MaximumPriceCents      *int64   `json:"maximum_price_cents,omitempty" jsonschema:"optional total price cap in cents"`
	Participants           int      `json:"participants,omitempty" jsonschema:"players sharing the court price"`
	Ranking                string   `json:"ranking,omitempty" jsonschema:"cheap, balanced, commute, or fair"`
}

type SearchOutput struct {
	Results []domain.SearchResult `json:"results"`
}

type VenueInput struct {
	Search string   `json:"search,omitempty"`
	Sports []string `json:"sports,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

type EventInput struct {
	WatchID string `json:"watch_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type CreateWatchInput struct {
	Name      string                      `json:"name" jsonschema:"required watch name"`
	Query     SearchInput                 `json:"query"`
	Targets   []domain.NotificationTarget `json:"targets,omitempty"`
	OneShot   bool                        `json:"one_shot,omitempty"`
	ExpiresAt string                      `json:"expires_at,omitempty" jsonschema:"optional RFC 3339 expiry"`
}

type ManualAvailabilityInput struct {
	ID                 string `json:"id,omitempty"`
	Sport              string `json:"sport"`
	VenueID            string `json:"venue_id"`
	Start              string `json:"start" jsonschema:"RFC 3339 start time"`
	End                string `json:"end" jsonschema:"RFC 3339 end time"`
	PriceCents         *int64 `json:"price_cents,omitempty"`
	BookingURL         string `json:"booking_url,omitempty"`
	MembershipRequired *bool  `json:"membership_required,omitempty"`
}

func New(service *app.Service, cfg config.Config, version string) (*Server, error) {
	if service == nil {
		return nil, fmt.Errorf("MCP service is required")
	}
	if version == "" {
		version = "dev"
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "courtsg", Version: version}, nil)
	addReadTools(server, service)
	if cfg.MCP.AllowWrites {
		addWriteTools(server, service)
	}
	return &Server{server: server}, nil
}

func (server *Server) RunStdio(ctx context.Context) error {
	return server.server.Run(ctx, &mcp.StdioTransport{})
}

func (server *Server) Raw() *mcp.Server { return server.server }

func addReadTools(server *mcp.Server, service *app.Service) {
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_search_availability", Description: "Search fresh normalized Singapore sports-facility availability from the local courtSG database. This never books or refreshes upstream sources."}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		query, err := input.query()
		if err != nil {
			return nil, SearchOutput{}, err
		}
		results, err := service.Search(ctx, query)
		return nil, SearchOutput{Results: results}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_list_venues", Description: "List venues already discovered from permitted sources, including provenance."}, func(ctx context.Context, _ *mcp.CallToolRequest, input VenueInput) (*mcp.CallToolResult, []domain.Venue, error) {
		venues, err := service.Venues(ctx, store.VenueFilter{Search: input.Search, Sports: input.Sports, Limit: input.Limit})
		return nil, venues, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_list_sources", Description: "Show courtSG source capabilities, policy state, and health. Disabled sources must not be treated as live availability."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []store.SourceRecord, error) {
		sources, err := service.Sources(ctx)
		return nil, sources, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_list_watches", Description: "List persistent local availability watches."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []domain.Watch, error) {
		watches, err := service.Watches(ctx, false)
		return nil, watches, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_list_events", Description: "List idempotent availability-match events generated by watches."}, func(ctx context.Context, _ *mcp.CallToolRequest, input EventInput) (*mcp.CallToolResult, []domain.Event, error) {
		events, err := service.Events(ctx, input.WatchID, input.Limit)
		return nil, events, err
	})
}

func addWriteTools(server *mcp.Server, service *app.Service) {
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_create_watch", Description: "Create a local persistent availability watch. Enabled only when mcp.allow_writes is true."}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateWatchInput) (*mcp.CallToolResult, domain.Watch, error) {
		query, err := input.Query.query()
		if err != nil {
			return nil, domain.Watch{}, err
		}
		var expiresAt *time.Time
		if strings.TrimSpace(input.ExpiresAt) != "" {
			parsed, err := time.Parse(time.RFC3339, input.ExpiresAt)
			if err != nil {
				return nil, domain.Watch{}, fmt.Errorf("parse expires_at: %w", err)
			}
			expiresAt = &parsed
		}
		watch, err := service.CreateWatch(ctx, domain.Watch{Name: input.Name, Query: query, Targets: input.Targets, Enabled: true, OneShot: input.OneShot, ExpiresAt: expiresAt})
		return nil, watch, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_add_manual_availability", Description: "Save user-authorized local availability. Enabled only when mcp.allow_writes is true; this makes no upstream request and never books."}, func(ctx context.Context, _ *mcp.CallToolRequest, input ManualAvailabilityInput) (*mcp.CallToolResult, domain.AvailabilitySlot, error) {
		start, err := time.Parse(time.RFC3339, input.Start)
		if err != nil {
			return nil, domain.AvailabilitySlot{}, fmt.Errorf("parse start: %w", err)
		}
		end, err := time.Parse(time.RFC3339, input.End)
		if err != nil {
			return nil, domain.AvailabilitySlot{}, fmt.Errorf("parse end: %w", err)
		}
		slots, err := service.ImportManualAvailability(ctx, []domain.AvailabilitySlot{{ID: input.ID, SportID: input.Sport, VenueID: input.VenueID, Start: start, End: end, PriceCents: input.PriceCents, BookingURL: input.BookingURL, MembershipRequired: input.MembershipRequired}})
		if err != nil {
			return nil, domain.AvailabilitySlot{}, err
		}
		return nil, slots[0], nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "courtsg_evaluate_watches", Description: "Evaluate enabled watches against local availability and create idempotent events. Enabled only when mcp.allow_writes is true."}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []app.WatchEvaluation, error) {
		results, err := service.EvaluateWatches(ctx)
		return nil, results, err
	})
}

func (input SearchInput) query() (domain.Query, error) {
	minutes := input.MinimumDurationMinutes
	if minutes == 0 {
		minutes = 60
	}
	if minutes < 1 || minutes > 24*60 {
		return domain.Query{}, fmt.Errorf("minimum_duration_minutes must be between 1 and 1440")
	}
	query := domain.Query{Sports: input.Sports, Sources: input.Sources, VenueIDs: input.VenueIDs, MinimumDuration: time.Duration(minutes) * time.Minute, MaximumPriceCents: input.MaximumPriceCents, Participants: input.Participants, Ranking: domain.RankingPreset(input.Ranking)}
	if input.Date != "" {
		location, err := time.LoadLocation(domain.SingaporeTimeZone)
		if err != nil {
			return domain.Query{}, err
		}
		date, err := time.ParseInLocation("2006-01-02", input.Date, location)
		if err != nil {
			return domain.Query{}, fmt.Errorf("parse date: %w", err)
		}
		query.StartDate, query.EndDate = &date, &date
	}
	return query, nil
}
