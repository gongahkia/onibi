package web

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/gongahkia/onibi/internal/approval"
)

type PendingApprovalsResponse struct {
	Approvals []map[string]any `json:"approvals"`
}

func (s *Server) handlePendingApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.requireHTTPAuth(w, r); !ok {
		return
	}
	if s.approvalQueue == nil {
		http.Error(w, "approval queue unavailable", http.StatusServiceUnavailable)
		return
	}
	pending, err := s.approvalQueue.Pending(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(pending))
	for _, a := range pending {
		payload := approvalEventPayload(approval.Event{Type: approval.EventRequested, Approval: *a})
		payload["session_url"] = "/s/" + url.PathEscape(a.SessionID)
		out = append(out, payload)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(PendingApprovalsResponse{Approvals: out})
}
