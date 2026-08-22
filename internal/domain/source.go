package domain

import "time"

type SourceStatus string

const (
	SourceEnabledPublicData   SourceStatus = "enabled_public_data"
	SourceEnabledOfficialAPI  SourceStatus = "enabled_official_api"
	SourceExperimental        SourceStatus = "experimental"
	SourceManualOnly          SourceStatus = "manual_only"
	SourceLinkOnly            SourceStatus = "link_only"
	SourceRequiresCredentials SourceStatus = "requires_credentials"
	SourceRequiresPermission  SourceStatus = "requires_permission"
	SourceDisabledRobots      SourceStatus = "disabled_by_robots"
	SourceDisabledTerms       SourceStatus = "disabled_by_terms"
	SourceDisabledUnknown     SourceStatus = "disabled_unknown_policy"
)

type Capabilities struct {
	VenueDiscovery bool `json:"venue_discovery"`
	Metadata       bool `json:"metadata"`
	Facilities     bool `json:"facilities"`
	Pricing        bool `json:"pricing"`
	OpeningHours   bool `json:"opening_hours"`
	Availability   bool `json:"availability"`
	BookingURL     bool `json:"booking_url"`
	GeographicData bool `json:"geographic_data"`
}

type SourcePolicy struct {
	Status         SourceStatus  `json:"status"`
	PermittedHosts []string      `json:"permitted_hosts"`
	Capabilities   Capabilities  `json:"capabilities"`
	AuthRequired   bool          `json:"auth_required"`
	PollFloor      time.Duration `json:"poll_floor"`
	AvailabilityMaxDays int      `json:"availability_max_days,omitempty"`
	Concurrency    int           `json:"concurrency"`
	Timeout        time.Duration `json:"timeout"`
	TermsURL       string        `json:"terms_url,omitempty"`
	RobotsURL      string        `json:"robots_url,omitempty"`
	EvidenceURLs   []string      `json:"evidence_urls,omitempty"`
	ReviewedAt     time.Time     `json:"reviewed_at"`
	Notes          string        `json:"notes,omitempty"`
}

func (policy SourcePolicy) AllowsNetwork() bool {
	return policy.Status == SourceEnabledPublicData || policy.Status == SourceEnabledOfficialAPI || policy.Status == SourceExperimental
}

type SourceInfo struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Operator string       `json:"operator"`
	Website  string       `json:"website,omitempty"`
	Policy   SourcePolicy `json:"policy"`
}

type HealthState string

const (
	HealthHealthy     HealthState = "healthy"
	HealthDegraded    HealthState = "degraded"
	HealthStale       HealthState = "stale"
	HealthDisabled    HealthState = "disabled"
	HealthCredentials HealthState = "credentials_required"
	HealthUnknown     HealthState = "unknown"
)

type SourceHealth struct {
	SourceID            string      `json:"source_id"`
	State               HealthState `json:"state"`
	LastAttempt         *time.Time  `json:"last_attempt,omitempty"`
	LastSuccess         *time.Time  `json:"last_success,omitempty"`
	LastCategory        string      `json:"last_category,omitempty"`
	LatencyMilliseconds int64       `json:"latency_ms,omitempty"`
	RecordsParsed       int         `json:"records_parsed,omitempty"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	BackoffUntil        *time.Time  `json:"backoff_until,omitempty"`
	LastError           string      `json:"last_error,omitempty"`
}
