package web

import (
	"encoding/json"
	"net/http"
)

type AnomalyAllowlistRequest struct {
	SessionID string `json:"session_id"`
	RuleName  string `json:"rule_name"`
	Evidence  string `json:"evidence"`
}

func (s *Server) handleAnomalyAllowlist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	if s.anomalyAllow == nil {
		http.Error(w, "anomaly allowlist unavailable", http.StatusServiceUnavailable)
		return
	}
	var req AnomalyAllowlistRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	msg, err := s.anomalyAllow(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": msg})
}
