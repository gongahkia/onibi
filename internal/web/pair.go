package web

import (
	"fmt"
	"net/http"

	"github.com/gongahkia/onibi/internal/setup"
)

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := r.PathValue("token")
	if token == "" || s.db == nil {
		pairFailed(w)
		return
	}
	if err := setup.Consume(r.Context(), s.db, token); err != nil {
		pairFailed(w)
		return
	}
	if _, err := s.CreateOwnerSession(r.Context(), w, r.UserAgent()); err != nil {
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func pairFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, "pair token expired or already used")
}
