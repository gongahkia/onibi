package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/timeline"
)

type SessionListOptions struct {
	IncludeRemote bool
}

type SessionSummary struct {
	ID                    string                     `json:"id"`
	HostID                string                     `json:"host_id,omitempty"`
	Agent                 string                     `json:"agent"`
	CWD                   string                     `json:"cwd"`
	StartedAt             string                     `json:"started_at"`
	LastActivity          string                     `json:"last_activity"`
	PendingApprovalsCount int                        `json:"pending_approvals_count"`
	RecoveryState         fleet.SessionRecoveryState `json:"recovery_state,omitempty"`
	RecoveryReason        string                     `json:"recovery_reason,omitempty"`
	RecoveryUpdatedAt     string                     `json:"recovery_updated_at,omitempty"`
	TokensUsed            int64                      `json:"tokens_used"`
	CostUSD               float64                    `json:"cost_usd"`
	RoleRequired          string                     `json:"role_required"`
	Remote                bool                       `json:"remote,omitempty"`
	PeerName              string                     `json:"peer_name,omitempty"`
	RemoteURL             string                     `json:"remote_url,omitempty"`
}

type SessionState string

const (
	SessionStateIdle             SessionState = "idle"
	SessionStateWorking          SessionState = "working"
	SessionStateAwaitingApproval SessionState = "awaiting-approval"
	SessionStateBlocked          SessionState = "blocked"

	sessionWorkingWindow = 3 * time.Second
)

type SessionsStatusResponse struct {
	GeneratedAt string               `json:"generated_at"`
	Sessions    []SessionStatus      `json:"sessions"`
	Counts      map[SessionState]int `json:"counts"`
}

type SessionStatus struct {
	ID                    string                     `json:"id"`
	HostID                string                     `json:"host_id,omitempty"`
	Agent                 string                     `json:"agent"`
	CWD                   string                     `json:"cwd,omitempty"`
	State                 SessionState               `json:"state"`
	LastActivity          string                     `json:"last_activity"`
	PendingApprovalsCount int                        `json:"pending_approvals_count"`
	RecoveryState         fleet.SessionRecoveryState `json:"recovery_state,omitempty"`
	RecoveryReason        string                     `json:"recovery_reason,omitempty"`
	RecoveryUpdatedAt     string                     `json:"recovery_updated_at,omitempty"`
	RoleRequired          string                     `json:"role_required"`
	Remote                bool                       `json:"remote,omitempty"`
	PeerName              string                     `json:"peer_name,omitempty"`
	RemoteURL             string                     `json:"remote_url,omitempty"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.sessionList == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	rows, err := s.sessionList(r.Context(), sessionListOptions(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (s *Server) handleSessionsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.sessionList == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := s.sessionsStatus(r.Context(), sessionListOptions(r), time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func sessionListOptions(r *http.Request) SessionListOptions {
	return SessionListOptions{
		IncludeRemote: includeRemoteSessions(r.URL.Query()["include"]),
	}
}

func includeRemoteSessions(values []string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "remote") {
				return true
			}
		}
	}
	return false
}

func (s *Server) sessionsStatus(ctx context.Context, opts SessionListOptions, now time.Time) (SessionsStatusResponse, error) {
	if s.sessionList == nil {
		return SessionsStatusResponse{}, nil
	}
	rows, err := s.sessionList(ctx, opts)
	if err != nil {
		return SessionsStatusResponse{}, err
	}
	latest, err := s.latestTimelineBySession(ctx)
	if err != nil {
		return SessionsStatusResponse{}, err
	}
	out := SessionsStatusResponse{
		GeneratedAt: now.UTC().Format(time.RFC3339Nano),
		Sessions:    make([]SessionStatus, 0, len(rows)),
		Counts: map[SessionState]int{
			SessionStateIdle:             0,
			SessionStateWorking:          0,
			SessionStateAwaitingApproval: 0,
			SessionStateBlocked:          0,
		},
	}
	for _, row := range rows {
		state := deriveSessionState(row, latest[row.ID], now)
		out.Counts[state]++
		out.Sessions = append(out.Sessions, SessionStatus{
			ID:                    row.ID,
			HostID:                row.HostID,
			Agent:                 row.Agent,
			CWD:                   row.CWD,
			State:                 state,
			LastActivity:          row.LastActivity,
			PendingApprovalsCount: row.PendingApprovalsCount,
			RecoveryState:         row.RecoveryState,
			RecoveryReason:        row.RecoveryReason,
			RecoveryUpdatedAt:     row.RecoveryUpdatedAt,
			RoleRequired:          row.RoleRequired,
			Remote:                row.Remote,
			PeerName:              row.PeerName,
			RemoteURL:             row.RemoteURL,
		})
	}
	return out, nil
}

func (s *Server) latestTimelineBySession(ctx context.Context) (map[string]*timeline.TimelineEvent, error) {
	out := map[string]*timeline.TimelineEvent{}
	if s.timeline == nil {
		return out, nil
	}
	events, err := s.timeline(ctx, defaultTimelineReplayEvents)
	if err != nil {
		return nil, err
	}
	for i := range events {
		ev := events[i]
		if ev.SessionID == "" {
			continue
		}
		current := out[ev.SessionID]
		if current == nil || timelineEventAfter(ev, *current) {
			out[ev.SessionID] = &ev
		}
	}
	return out, nil
}

func deriveSessionState(row SessionSummary, latest *timeline.TimelineEvent, now time.Time) SessionState {
	if row.PendingApprovalsCount > 0 {
		return SessionStateAwaitingApproval
	}
	if latest != nil {
		switch latest.Kind {
		case timeline.KindAnomaly:
			return SessionStateBlocked
		case timeline.KindToolCall:
			if timelineEventRecent(*latest, now, sessionWorkingWindow) {
				return SessionStateWorking
			}
		}
	}
	if ts, ok := parseWebTime(row.LastActivity); ok && !now.Before(ts) && now.Sub(ts) <= sessionWorkingWindow {
		return SessionStateWorking
	}
	return SessionStateIdle
}

func timelineEventAfter(a, b timeline.TimelineEvent) bool {
	at, aok := parseWebTime(a.TS)
	bt, bok := parseWebTime(b.TS)
	if aok && bok {
		return at.After(bt)
	}
	return a.Offset > b.Offset
}

func timelineEventRecent(ev timeline.TimelineEvent, now time.Time, window time.Duration) bool {
	ts, ok := parseWebTime(ev.TS)
	return ok && !now.Before(ts) && now.Sub(ts) <= window
}

func parseWebTime(raw string) (time.Time, bool) {
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err == nil {
		return ts, true
	}
	ts, err = time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return ts, err == nil
}
