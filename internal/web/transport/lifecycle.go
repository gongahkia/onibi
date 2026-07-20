package transport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

type LifecycleState string

const (
	LifecycleNew     LifecycleState = "new"
	LifecycleStarted LifecycleState = "started"
	LifecycleHealthy LifecycleState = "healthy"
	LifecycleStopped LifecycleState = "stopped"
	LifecycleFailed  LifecycleState = "failed"
)

type LifecycleDiagnostic struct {
	Operation string
	Code      DiagnosticCode
	Message   string
	At        time.Time
}

type HealthReport struct {
	State   LifecycleState
	Healthy bool
	Targets []string
}

type EnrollmentCandidate struct {
	Endpoint           fleet.Endpoint
	RequiresOwnerProof bool
}

type Lifecycle interface {
	Start(context.Context) (Resolved, error)
	Resolve(context.Context) (Resolved, error)
	Health(context.Context) (HealthReport, error)
	Pair(string) ([]string, error)
	Reconnect(context.Context) (Resolved, error)
	Enrollment() (EnrollmentCandidate, error)
	Shutdown(context.Context) error
	Diagnostics() []LifecycleDiagnostic
}

type Session struct {
	mu          sync.Mutex
	opts        ResolverOptions
	state       LifecycleState
	resolved    Resolved
	diagnostics []LifecycleDiagnostic
}

func NewLifecycle(opts ResolverOptions) *Session {
	return &Session{opts: opts, state: LifecycleNew}
}

func (s *Session) Start(ctx context.Context) (Resolved, error) { return s.resolve(ctx, "start") }

func (s *Session) Resolve(ctx context.Context) (Resolved, error) { return s.resolve(ctx, "resolve") }

func (s *Session) resolve(ctx context.Context, operation string) (Resolved, error) {
	s.mu.Lock()
	if s.state == LifecycleStarted || s.state == LifecycleHealthy {
		resolved := s.resolved
		s.mu.Unlock()
		return resolved, nil
	}
	opts := s.opts
	s.mu.Unlock()
	resolved, err := resolveTransport(ctx, opts)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state = LifecycleFailed
		s.recordLocked(operation, err)
		return Resolved{}, err
	}
	s.resolved = resolved
	s.state = LifecycleStarted
	return resolved, nil
}

func (s *Session) Health(ctx context.Context) (HealthReport, error) {
	s.mu.Lock()
	state := s.state
	resolved := s.resolved
	s.mu.Unlock()
	if state != LifecycleStarted && state != LifecycleHealthy {
		err := errors.New("transport lifecycle is not started")
		s.mu.Lock()
		s.recordLocked("health", err)
		s.mu.Unlock()
		return HealthReport{State: state}, err
	}
	if err := resolved.Health(ctx); err != nil {
		s.mu.Lock()
		s.state = LifecycleFailed
		s.recordLocked("health", err)
		s.mu.Unlock()
		return HealthReport{State: LifecycleFailed, Targets: resolved.TargetURLs()}, err
	}
	s.mu.Lock()
	s.state = LifecycleHealthy
	s.mu.Unlock()
	return HealthReport{State: LifecycleHealthy, Healthy: true, Targets: resolved.TargetURLs()}, nil
}

func (s *Session) Pair(token string) ([]string, error) {
	s.mu.Lock()
	state := s.state
	resolved := s.resolved
	s.mu.Unlock()
	if state != LifecycleStarted && state != LifecycleHealthy {
		return nil, errors.New("transport lifecycle is not started")
	}
	if token == "" {
		return nil, errors.New("pair token required")
	}
	urls := resolved.URLs(token)
	if len(urls) == 0 {
		return nil, errors.New("transport produced no pair URL")
	}
	return urls, nil
}

func (s *Session) Reconnect(ctx context.Context) (Resolved, error) {
	s.mu.Lock()
	resolved := s.resolved
	active := s.state == LifecycleStarted || s.state == LifecycleHealthy || s.state == LifecycleFailed
	s.state = LifecycleNew
	s.resolved = Resolved{}
	s.mu.Unlock()
	if active {
		if err := resolved.Disable(ctx); err != nil {
			s.mu.Lock()
			s.recordLocked("reconnect.shutdown", err)
			s.mu.Unlock()
			return Resolved{}, err
		}
	}
	return s.resolve(ctx, "reconnect")
}

func (s *Session) Enrollment() (EnrollmentCandidate, error) {
	s.mu.Lock()
	state := s.state
	resolved := s.resolved
	s.mu.Unlock()
	if state != LifecycleStarted && state != LifecycleHealthy {
		return EnrollmentCandidate{}, errors.New("transport lifecycle is not started")
	}
	endpoint := fleet.Endpoint{URL: resolved.RedactedBaseURL()}
	switch resolved.Mode {
	case ModeLAN:
		for _, target := range resolved.TargetURLs() {
			candidate := fleet.Endpoint{Kind: fleet.EndpointMesh, URL: target}
			if candidate.Validate() == nil {
				endpoint = candidate
				break
			}
		}
		if endpoint.Kind == "" {
			return EnrollmentCandidate{}, errors.New("transport does not provide a valid fleet enrollment endpoint")
		}
	case ModeTailscalePrivate, ModeWireGuard, ModeZeroTier:
		endpoint.Kind = fleet.EndpointMesh
	case ModeTailscale, ModeCloudflareQuick, ModeNgrok:
		endpoint.Kind = fleet.EndpointRelay
	default:
		return EnrollmentCandidate{}, errors.New("transport does not provide a fleet enrollment endpoint")
	}
	if err := endpoint.Validate(); err != nil {
		return EnrollmentCandidate{}, err
	}
	return EnrollmentCandidate{Endpoint: endpoint, RequiresOwnerProof: true}, nil
}

func (s *Session) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	resolved := s.resolved
	active := s.state == LifecycleStarted || s.state == LifecycleHealthy || s.state == LifecycleFailed
	s.state = LifecycleStopped
	s.resolved = Resolved{}
	s.mu.Unlock()
	if !active {
		return nil
	}
	if err := resolved.Disable(ctx); err != nil {
		s.mu.Lock()
		s.recordLocked("shutdown", err)
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Session) Diagnostics() []LifecycleDiagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LifecycleDiagnostic(nil), s.diagnostics...)
}

func (s *Session) recordLocked(operation string, err error) {
	entry := LifecycleDiagnostic{Operation: operation, Message: err.Error(), At: time.Now().UTC()}
	var diagnostic *DiagnosticError
	if errors.As(err, &diagnostic) {
		entry.Code = diagnostic.Code
	}
	s.diagnostics = append(s.diagnostics, entry)
}
