package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
)

type shareCreateRequest struct {
	SessionID  string `json:"session_id"`
	TTL        string `json:"ttl"`
	MaxViewers int    `json:"max_viewers"`
}

type shareCreateResponse struct {
	URL        string `json:"url"`
	QRPNGData  string `json:"qr_png_data"`
	SessionID  string `json:"session_id"`
	Role       string `json:"role"`
	ExpiresAt  string `json:"expires_at"`
	TTL        string `json:"ttl"`
	MaxViewers int    `json:"max_viewers"`
}

type shareListResponse struct {
	Viewers []shareViewerResponse `json:"viewers"`
}

type shareViewerResponse struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
}

type shareRevokeRequest struct {
	SessionID string `json:"session_id"`
	ViewerID  string `json:"viewer_id"`
}

func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleShareList(w, r)
	case http.MethodPost:
		s.handleShareCreate(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	ownerSessionID, ok := s.requireOwnerHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	var req shareCreateRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" || !containsSessionID(s.activeSessionIDs(), sessionID) {
		writeControlError(w, "session not found", http.StatusNotFound)
		return
	}
	ttl, ok := parseShareTTL(req.TTL)
	if !ok {
		writeControlError(w, "bad ttl", http.StatusBadRequest)
		return
	}
	if req.MaxViewers < 1 || req.MaxViewers > 5 {
		writeControlError(w, "max viewers must be 1-5", http.StatusBadRequest)
		return
	}
	token, err := setup.NewViewerToken(r.Context(), s.db, sessionID, ttl, req.MaxViewers)
	if err != nil {
		s.log.Warn("web share create failed", "request_id", requestID(r), "session_id", sessionID, "err", err, "remote", remoteHost(r.RemoteAddr))
		writeControlError(w, "share unavailable", http.StatusInternalServerError)
		return
	}
	rawURL := viewerShareURLForRequest(r, token, sessionID)
	qrData, err := qrPNGDataURL(rawURL)
	if err != nil {
		s.log.Warn("web share qr failed", "request_id", requestID(r), "session_id", sessionID, "err", err, "remote", remoteHost(r.RemoteAddr))
		writeControlError(w, "qr unavailable", http.StatusInternalServerError)
		return
	}
	expiresAt := time.Now().Add(ttl).UTC().Format(time.RFC3339)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(shareCreateResponse{
		URL:        rawURL,
		QRPNGData:  qrData,
		SessionID:  sessionID,
		Role:       store.PairRoleViewer,
		ExpiresAt:  expiresAt,
		TTL:        ttl.String(),
		MaxViewers: req.MaxViewers,
	})
}

func (s *Server) handleShareList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireOwnerHTTPAuth(w, r); !ok {
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" || !containsSessionID(s.activeSessionIDs(), sessionID) {
		writeControlError(w, "session not found", http.StatusNotFound)
		return
	}
	viewers, err := s.shareViewers(r.Context(), sessionID)
	if err != nil {
		s.log.Warn("web share list failed", "request_id", requestID(r), "session_id", sessionID, "err", err, "remote", remoteHost(r.RemoteAddr))
		writeControlError(w, "share unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(shareListResponse{Viewers: viewers})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
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
	var req shareRevokeRequest
	if !s.readJSONBody(w, r, ownerSessionID, &req) {
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	viewerID := strings.TrimSpace(req.ViewerID)
	viewer, ok, err := s.db.WebSession(r.Context(), viewerID)
	if err != nil {
		s.log.Warn("web share revoke lookup failed", "request_id", requestID(r), "session_id", sessionID, "viewer_id", shortLogID(viewerID), "err", err, "remote", remoteHost(r.RemoteAddr))
		writeControlError(w, "share unavailable", http.StatusInternalServerError)
		return
	}
	if !ok || !activeViewerForSession(viewer, sessionID) {
		writeControlError(w, "viewer not found", http.StatusNotFound)
		return
	}
	revoked, err := s.db.RevokeWebSessionWithRole(r.Context(), viewerID, store.PairRoleViewer)
	if err != nil {
		s.log.Warn("web share revoke failed", "request_id", requestID(r), "session_id", sessionID, "viewer_id", shortLogID(viewerID), "err", err, "remote", remoteHost(r.RemoteAddr))
		writeControlError(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	if !revoked {
		writeControlError(w, "viewer not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) shareViewers(ctx context.Context, sessionID string) ([]shareViewerResponse, error) {
	devices, err := s.db.ListWebSessions(ctx, false)
	if err != nil {
		return nil, err
	}
	out := []shareViewerResponse{}
	for _, d := range devices {
		if activeViewerForSession(d, sessionID) {
			out = append(out, shareViewerResponse{
				ID:         d.SessionID,
				Label:      d.DeviceLabel,
				CreatedAt:  formatShareTime(d.CreatedAt),
				LastSeenAt: formatShareTime(d.LastSeenAt),
				ExpiresAt:  formatShareTime(d.ShareExpiresAt),
			})
		}
	}
	return out, nil
}

func activeViewerForSession(session store.WebSession, sessionID string) bool {
	return !session.Revoked &&
		session.Role == store.PairRoleViewer &&
		session.ShareSessionID == sessionID &&
		(session.ShareExpiresAt.IsZero() || session.ShareExpiresAt.After(time.Now()))
}

func parseShareTTL(raw string) (time.Duration, bool) {
	ttl, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	switch ttl {
	case 5 * time.Minute, 30 * time.Minute, time.Hour, 4 * time.Hour, 24 * time.Hour:
		return ttl, true
	default:
		return 0, false
	}
}

func viewerShareURLForRequest(r *http.Request, token, sessionID string) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded == "https" || forwarded == "http" {
		scheme = forwarded
	}
	base := url.URL{Scheme: scheme, Host: r.Host, Path: "/pair/" + token}
	return base.String() + "#/s/" + url.PathEscape(sessionID)
}

func qrPNGDataURL(value string) (string, error) {
	png, err := qrcode.Encode(value, qrcode.Low, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func formatShareTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func shortLogID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
