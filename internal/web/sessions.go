package web

import (
	"encoding/json"
	"net/http"
)

type SessionSummary struct {
	ID                    string  `json:"id"`
	Agent                 string  `json:"agent"`
	CWD                   string  `json:"cwd"`
	StartedAt             string  `json:"started_at"`
	LastActivity          string  `json:"last_activity"`
	PendingApprovalsCount int     `json:"pending_approvals_count"`
	TokensUsed            int64   `json:"tokens_used"`
	CostUSD               float64 `json:"cost_usd"`
	RoleRequired          string  `json:"role_required"`
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
	rows, err := s.sessionList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}
