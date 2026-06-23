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

var errUnauthorized = errors.New("unauthorized")

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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return sessionID, true
}

func (s *Server) requireWSAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	sessionID, err := s.authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if token := r.URL.Query().Get("token"); token == "" || token != sessionID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return sessionID, true
}

func (s *Server) authenticate(r *http.Request) (string, error) {
	if s.db == nil {
		return "", errUnauthorized
	}
	cookie, err := r.Cookie(OwnerCookieName)
	if err != nil || cookie.Value == "" {
		return "", errUnauthorized
	}
	ok, err := s.db.TouchWebSession(r.Context(), cookie.Value, time.Now())
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errUnauthorized
	}
	return cookie.Value, nil
}

func newSessionID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
