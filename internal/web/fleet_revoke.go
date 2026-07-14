package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func (s *Server) handleFleetRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerSessionID, ok := s.requireOwnerHTTPAuth(w, r)
	if !ok || !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	if s.approvalQueue == nil {
		http.Error(w, "fleet unavailable", http.StatusServiceUnavailable)
		return
	}
	var req fleet.RevocationRequest
	if !s.readJSONBodyLimit(w, r, ownerSessionID, &req, 8<<10) {
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ownerID, err := s.db.FleetOwnerID(r.Context())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	result, revoked, err := s.db.FleetHostEmergencyRevoke(r.Context(), ownerID, req.HostID, time.Now().UTC())
	if err != nil {
		http.Error(w, "fleet unavailable", http.StatusInternalServerError)
		return
	}
	if !revoked {
		http.Error(w, "fleet host unavailable", http.StatusConflict)
		return
	}
	s.closeFleetLink(result.Host.ID)
	s.approvalQueue.InvalidateWaiters(result.ApprovalIDs, "fleet emergency host revocation")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"host": result.Host, "web_sessions_revoked": result.WebSessionsRevoked, "pending_actions_cancelled": len(result.ApprovalIDs)})
}
