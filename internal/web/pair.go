package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gongahkia/onibi/internal/setup"
)

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := r.PathValue("token")
	if token == "" || s.db == nil {
		s.log.Warn("web pair failed", "request_id", requestID(r), "reason", "missing_token_or_db", "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
		pairFailed(w)
		return
	}
	claim, err := setup.Claim(r.Context(), s.db, token)
	if err != nil {
		s.log.Warn("web pair failed", "request_id", requestID(r), "reason", err.Error(), "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
		pairFailed(w)
		return
	}
	sessionID, err := s.CreateWebSession(r.Context(), w, r.UserAgent(), claim.Role)
	if err != nil {
		s.log.Error("web pair session create failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	if s.relayKeys != nil {
		bound, err := s.relayKeys.BindSession(r.Context(), s.db, token, sessionID)
		if err != nil {
			s.log.Warn("web relay key bind failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		}
		if s.requireE2E && !bound {
			s.log.Warn("web relay key missing", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr))
			http.Error(w, "relay key missing", http.StatusUnauthorized)
			return
		}
	}
	s.log.Info("web pair accepted", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr), "role", claim.Role, "user_agent", trimForLog(r.UserAgent(), 160))
	pairAccepted(w, claim.SessionID)
}

func pairFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, "pair token expired or already used")
}

func pairAccepted(w http.ResponseWriter, sessionID string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	target := "/"
	if sessionID != "" {
		target = "/s/" + url.PathEscape(sessionID)
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><title>Onibi paired</title><body><p>Paired. Opening Onibi...</p><script>const h=location.hash;location.replace(h.startsWith("#/")?h.slice(1):(h?"/"+h:%s))</script><p><a href="/">Open Onibi</a></p></body>`, strconv.Quote(target))
}
