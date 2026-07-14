package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

const (
	OwnerCookieName   = "onibi_owner"
	csrfHeaderName    = "X-Onibi-CSRF"
	ownerCookieMaxAge = 30 * 24 * 60 * 60
)

var (
	errAuthNoDB          = errors.New("db_unavailable")
	errAuthMissingCookie = errors.New("missing_owner_cookie")
	errAuthEmptyCookie   = errors.New("empty_owner_cookie")
	errAuthInvalidCookie = errors.New("invalid_owner_session")
	errAuthForbiddenRole = errors.New("forbidden_session_role")
	errAuthBadVerifier   = errors.New("bad_relay_verifier")
)

type authenticatedSession struct {
	ID   string
	Role string
}

func (s *Server) CreateOwnerSession(ctx context.Context, w http.ResponseWriter, deviceLabel string) (string, error) {
	if s.db == nil {
		return "", errors.New("web: db is required")
	}
	sessionID, err := newSessionID()
	if err != nil {
		return "", err
	}
	if err := s.db.PutWebSessionWithRole(ctx, sessionID, deviceLabel, "owner", time.Now()); err != nil {
		return "", err
	}
	setOwnerCookie(w, sessionID)
	return sessionID, nil
}

func (s *Server) CreateWebSession(ctx context.Context, w http.ResponseWriter, deviceLabel, role string) (string, error) {
	if s.db == nil {
		return "", errors.New("web: db is required")
	}
	sessionID, err := newSessionID()
	if err != nil {
		return "", err
	}
	if err := s.db.PutWebSessionWithRole(ctx, sessionID, deviceLabel, role, time.Now()); err != nil {
		return "", err
	}
	setOwnerCookie(w, sessionID)
	return sessionID, nil
}

func (s *Server) CreateViewerSession(ctx context.Context, w http.ResponseWriter, deviceLabel, shareSessionID string, shareExpiresAt time.Time) (string, error) {
	if s.db == nil {
		return "", errors.New("web: db is required")
	}
	sessionID, err := newSessionID()
	if err != nil {
		return "", err
	}
	if err := s.db.PutViewerWebSession(ctx, sessionID, deviceLabel, shareSessionID, shareExpiresAt, time.Now()); err != nil {
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
	auth, ok := s.requireHTTPAuthInfo(w, r)
	if !ok {
		return "", false
	}
	return auth.ID, true
}

func (s *Server) requireHTTPAuthInfo(w http.ResponseWriter, r *http.Request) (authenticatedSession, bool) {
	auth, err := s.authenticate(r)
	if err != nil {
		s.log.Warn("web http auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return authenticatedSession{}, false
	}
	return auth, true
}

func (s *Server) requireOwnerHTTPAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	auth, err := s.authenticate(r)
	if err != nil {
		s.log.Warn("web http auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if auth.Role != store.PairRoleOwner {
		s.log.Warn("web http auth forbidden", "request_id", requestID(r), "reason", errAuthForbiddenRole.Error(), "role", auth.Role, "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	return auth.ID, true
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	got := r.Header.Get(csrfHeaderName)
	want := csrfTokenForSession(sessionID)
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		s.log.Warn("web csrf failed", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr), "header_present", got != "")
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func csrfTokenForSession(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(sessionID))
	mac.Write([]byte("onibi csrf token v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func CSRFTokenForSession(sessionID string) string { return csrfTokenForSession(sessionID) }

func (s *Server) requireWSAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	auth, ok := s.requireWSAuthInfo(w, r)
	if !ok {
		return "", false
	}
	return auth.ID, true
}

func (s *Server) requireWSAuthInfo(w http.ResponseWriter, r *http.Request) (authenticatedSession, bool) {
	auth, err := s.authenticate(r)
	if err != nil {
		s.log.Warn("web ws auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "token_present", r.URL.Query().Get("token") != "", "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return authenticatedSession{}, false
	}
	if token := r.URL.Query().Get("token"); token == "" || token != auth.ID {
		s.log.Warn("web ws auth failed", "request_id", requestID(r), "reason", "token_mismatch", "cookie_present", true, "token_present", token != "", "path", safeRequestPath(r.URL.Path), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return authenticatedSession{}, false
	}
	return auth, true
}

func (s *Server) authenticate(r *http.Request) (authenticatedSession, error) {
	if s.db == nil {
		return authenticatedSession{}, errAuthNoDB
	}
	cookie, err := r.Cookie(OwnerCookieName)
	if err != nil || cookie.Value == "" {
		if err != nil {
			return authenticatedSession{}, errAuthMissingCookie
		}
		return authenticatedSession{}, errAuthEmptyCookie
	}
	ok, err := s.db.TouchWebSession(r.Context(), cookie.Value, time.Now())
	if err != nil {
		return authenticatedSession{}, err
	}
	if !ok {
		return authenticatedSession{}, errAuthInvalidCookie
	}
	role, ok, err := s.db.WebSessionRole(r.Context(), cookie.Value)
	if err != nil {
		return authenticatedSession{}, err
	}
	if !ok {
		return authenticatedSession{}, errAuthInvalidCookie
	}
	return authenticatedSession{ID: cookie.Value, Role: role}, nil
}

func ownerCookiePresent(r *http.Request) bool {
	cookie, err := r.Cookie(OwnerCookieName)
	return err == nil && cookie.Value != ""
}

func (s *Server) verifyRelayAttach(ctx context.Context, sessionID, encoded string) error {
	if s.db == nil {
		return errAuthNoDB
	}
	if encoded == "" {
		return errAuthBadVerifier
	}
	got, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return errAuthBadVerifier
	}
	want, ok, err := s.db.WebSessionKeyVerifier(ctx, sessionID)
	if err != nil {
		return err
	}
	if !ok || subtle.ConstantTimeCompare(got, want) != 1 {
		return errAuthBadVerifier
	}
	return nil
}

func newSessionID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
