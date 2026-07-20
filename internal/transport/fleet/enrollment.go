package fleettransport

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

const (
	EnrollmentAdapterVersion   = fleet.ProtocolVersion
	EnrollmentPlanTTL          = 10 * time.Minute
	enrollmentPlanMaxClockSkew = 2 * time.Minute
)

type Adapter string

const (
	AdapterMesh  Adapter = "mesh"
	AdapterRelay Adapter = "relay"
)

type EnrollmentPlan struct {
	Version   uint16    `json:"version"`
	Adapter   Adapter   `json:"adapter"`
	Address   string    `json:"address"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewEnrollmentPlan(adapter, address string, now time.Time) (EnrollmentPlan, error) {
	now = now.UTC()
	plan := EnrollmentPlan{
		Version:   EnrollmentAdapterVersion,
		Adapter:   Adapter(strings.ToLower(strings.TrimSpace(adapter))),
		Address:   strings.TrimSpace(address),
		IssuedAt:  now,
		ExpiresAt: now.Add(EnrollmentPlanTTL),
	}
	if _, err := plan.Resolve(now); err != nil {
		return EnrollmentPlan{}, err
	}
	return plan, nil
}

func (p EnrollmentPlan) Resolve(now time.Time) (fleet.Endpoint, error) {
	if p.Version != EnrollmentAdapterVersion {
		return fleet.Endpoint{}, fmt.Errorf("fleet enrollment adapter version %d is incompatible with %d", p.Version, EnrollmentAdapterVersion)
	}
	if p.IssuedAt.IsZero() || p.ExpiresAt.IsZero() || !p.ExpiresAt.After(p.IssuedAt) || p.IssuedAt.After(now.UTC().Add(enrollmentPlanMaxClockSkew)) || !p.ExpiresAt.After(now.UTC()) || p.ExpiresAt.Sub(p.IssuedAt) > EnrollmentPlanTTL {
		return fleet.Endpoint{}, errors.New("fleet enrollment adapter plan expired or invalid")
	}
	endpoint := fleet.Endpoint{URL: strings.TrimSpace(p.Address)}
	switch p.Adapter {
	case AdapterMesh:
		endpoint.Kind = fleet.EndpointMesh
	case AdapterRelay:
		endpoint.Kind = fleet.EndpointRelay
	default:
		return fleet.Endpoint{}, fmt.Errorf("unsupported fleet enrollment adapter %q", p.Adapter)
	}
	if err := endpoint.Validate(); err != nil {
		return fleet.Endpoint{}, err
	}
	return endpoint, nil
}

func (p EnrollmentPlan) ApplyToHost(host fleet.Host, now time.Time) (fleet.Host, error) {
	if host.State == fleet.HostStateRevoked {
		return fleet.Host{}, errors.New("revoked fleet host cannot select an enrollment adapter")
	}
	endpoint, err := p.Resolve(now)
	if err != nil {
		return fleet.Host{}, err
	}
	host.Endpoint = endpoint
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return fleet.Host{}, err
	}
	return host, nil
}
