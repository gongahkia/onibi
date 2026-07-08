package web

import (
	"encoding/json"
	"net/http"
)

type TrustRuntimeRequest struct {
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	Path      string `json:"path"`
	Agent     string `json:"agent"`
	Expires   string `json:"expires"`
}

func (s *Server) handleTrustRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireOwnerHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	if s.trustRuntime == nil {
		http.Error(w, "trust runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	var req TrustRuntimeRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	msg, err := s.trustRuntime(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": msg})
}
