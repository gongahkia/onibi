package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"
)

const (
	OwnerCookieName   = "onibi_owner"
	ownerCookieMaxAge = 30 * 24 * 60 * 60
)

var (
	errAuthNoDB          = errors.New("db_unavailable")
	errAuthMissingCookie = errors.New("missing_owner_cookie")
	errAuthEmptyCookie   = errors.New("empty_owner_cookie")
	errAuthInvalidCookie = errors.New("invalid_owner_session")
)

func (s *Server) CreateOwnerSession(ctx context.Context, w http.ResponseWriter, deviceLabel string) (string, error) {
	if s.db == nil {
		return "", errors.New("web: db is required")
	}
	sessionID, err := newSessionID()
	if err != nil {
		return "", err
	}
	if err := s.db.PutWebSession(ctx, sessionID, deviceLabel, time.Now()); err != nil {
		return "", err
	}
	setOwnerCookie(w, sessionID)
	return sessionID, nil
}

func setOwnerCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     OwnerCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   ownerCookieMaxAge,
		Expires:  time.Now().Add(ownerCookieMaxAge * time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) requireHTTPAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	sessionID, err := s.authenticate(r)
	if err != nil {
		s.log.Warn("web http auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return sessionID, true
}

func (s *Server) requireWSAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	sessionID, err := s.authenticate(r)
	if err != nil {
		s.log.Warn("web ws auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "token_present", r.URL.Query().Get("token") != "", "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if token := r.URL.Query().Get("token"); token == "" || token != sessionID {
		s.log.Warn("web ws auth failed", "request_id", requestID(r), "reason", "token_mismatch", "cookie_present", true, "token_present", token != "", "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return sessionID, true
}

func (s *Server) authenticate(r *http.Request) (string, error) {
	if s.db == nil {
		return "", errAuthNoDB
	}
	cookie, err := r.Cookie(OwnerCookieName)
	if err != nil || cookie.Value == "" {
		if err != nil {
			return "", errAuthMissingCookie
		}
		return "", errAuthEmptyCookie
	}
	ok, err := s.db.TouchWebSession(r.Context(), cookie.Value, time.Now())
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errAuthInvalidCookie
	}
	return cookie.Value, nil
}

func ownerCookiePresent(r *http.Request) bool {
	cookie, err := r.Cookie(OwnerCookieName)
	return err == nil && cookie.Value != ""
}

func newSessionID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
