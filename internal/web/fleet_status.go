package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func (s *Server) handleFleetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil || s.sessionList == nil || s.approvalQueue == nil {
		http.Error(w, "fleet status unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.requireOwnerHTTPAuth(w, r); !ok {
		return
	}
	now := time.Now().UTC()
	ownerID, err := s.db.FleetOwnerID(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if _, err := s.db.FleetHostMarkStaleBefore(r.Context(), now.Add(-fleet.HostStaleAfter)); err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	hosts, err := s.db.FleetHostList(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	ownedHosts := make([]fleet.Host, 0, len(hosts))
	for _, host := range hosts {
		if host.OwnerID == ownerID {
			ownedHosts = append(ownedHosts, host)
		}
	}
	sessionStatus, err := s.sessionsStatus(r.Context(), SessionListOptions{IncludeRemote: true}, now)
	if err != nil {
		http.Error(w, "fleet status unavailable", http.StatusInternalServerError)
		return
	}
	pending, err := s.approvalQueue.Pending(r.Context())
	if err != nil {
		http.Error(w, "fleet status unavailable", http.StatusInternalServerError)
		return
	}
	status := fleet.HomeStatus{
		Version:          fleet.ProtocolVersion,
		GeneratedAt:      now,
		Hosts:            ownedHosts,
		Sessions:         make([]fleet.HomeSessionStatus, 0, len(sessionStatus.Sessions)),
		PendingApprovals: make([]fleet.HomeApprovalStatus, 0, len(pending)),
	}
	for _, session := range sessionStatus.Sessions {
		lastActivity, ok := parseWebTime(session.LastActivity)
		if !ok {
			http.Error(w, "fleet status unavailable", http.StatusInternalServerError)
			return
		}
		homeSession := fleet.HomeSessionStatus{
			ID:               session.ID,
			Agent:            session.Agent,
			State:            string(session.State),
			LastActivity:     lastActivity,
			PendingApprovals: session.PendingApprovalsCount,
			Remote:           session.Remote,
			PeerName:         session.PeerName,
		}
		if session.RecoveryState != "" {
			recoveryUpdatedAt, ok := parseWebTime(session.RecoveryUpdatedAt)
			if !ok {
				http.Error(w, "fleet status unavailable", http.StatusInternalServerError)
				return
			}
			homeSession.RecoveryState = session.RecoveryState
			homeSession.RecoveryReason = session.RecoveryReason
			homeSession.RecoveryUpdatedAt = recoveryUpdatedAt
		}
		status.Sessions = append(status.Sessions, homeSession)
	}
	for _, approval := range pending {
		status.PendingApprovals = append(status.PendingApprovals, fleet.HomeApprovalStatus{
			ID:        approval.ID,
			SessionID: approval.SessionID,
			Agent:     approval.Agent,
			Tool:      approval.Tool,
			State:     approval.State,
			CreatedAt: approval.CreatedAt.UTC(),
			ExpiresAt: approval.ExpiresAt.UTC(),
		})
	}
	if err := status.Validate(); err != nil {
		http.Error(w, "fleet status unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}
