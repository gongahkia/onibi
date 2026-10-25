// Package web provides the authenticated, same-origin HTTP boundary for native
// Ghostty control. Terminal operations are delegated to an explicit backend.
package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	pairingLifetime  = 10 * time.Minute
	sessionLifetime  = 12 * time.Hour
	maxBodyBytes     = 64 << 10
	maxInputBytes    = 16 << 10
	maxSnapshotBytes = 256 << 10
	actionTimeout    = 30 * time.Second
)

type Backend interface {
	Targets(context.Context) ([]ghostty.Target, error)
	Snapshot(context.Context, string) (ghostty.Snapshot, error)
	Execute(context.Context, ghostty.Action) (ghostty.Result, error)
}

type Options struct {
	Backend      Backend
	DB           *store.DB
	PublicOrigin string
	Assets       fs.FS
	// ProxyHosts are explicit loopback upstream authorities used by a local
	// HTTPS proxy. They are accepted only from a loopback peer. Forwarded
	// headers never determine the trusted public origin or cookie security.
	ProxyHosts []string
}

type browserSession struct {
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Server struct {
	backend     Backend
	db          *store.DB
	origin      string
	host        string
	proxyHosts  map[string]bool
	secure      bool
	cookieName  string
	assets      http.Handler
	execution   chan struct{}
	mu          sync.Mutex
	pairHash    [sha256.Size]byte
	pairExpires time.Time
	pairUsed    bool
	pairURL     string
	sessions    map[[sha256.Size]byte]browserSession
	now         func() time.Time
}

// New creates a server. The caller must hold an exclusive process lock for the
// supplied database while serving; startup marks interrupted claims uncertain.
func New(opts Options) (*Server, error) {
	if opts.Backend == nil || opts.DB == nil {
		return nil, errors.New("web backend and state database are required")
	}
	origin, err := parseOrigin(opts.PublicOrigin)
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	s := &Server{
		backend: opts.Backend, db: opts.DB, origin: origin.String(), host: origin.Host,
		secure: origin.Scheme == "https", cookieName: "onibi_local_session",
		proxyHosts: map[string]bool{}, sessions: map[[sha256.Size]byte]browserSession{},
		execution: make(chan struct{}, 1), now: time.Now,
		pairHash: sha256.Sum256([]byte(token)), pairExpires: time.Now().Add(pairingLifetime),
	}
	if s.secure {
		s.cookieName = "__Host-onibi_session"
	}
	for _, host := range opts.ProxyHosts {
		u, err := url.Parse("http://" + host)
		if err != nil || u.Host != host || u.Path != "" || u.User != nil || !loopbackHost(u.Hostname()) {
			return nil, errors.New("web proxy hosts must be explicit loopback authorities")
		}
		s.proxyHosts[host] = true
	}
	if opts.Assets != nil {
		s.assets = http.FileServer(http.FS(opts.Assets))
	}
	link := *origin
	link.Path = "/"
	link.Fragment = "token=" + token
	s.pairURL = link.String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.prepareReceipts(ctx); err != nil {
		return nil, fmt.Errorf("prepare web command receipts: %w", err)
	}
	return s, nil
}

// PairingURL returns the local startup secret once. It must be printed only to
// the local operator, never served over HTTP or written to application logs.
func (s *Server) PairingURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	link := s.pairURL
	s.pairURL = ""
	return link
}

func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("web origin must contain only a scheme and authority")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !loopbackHost(u.Hostname())) {
		return nil, errors.New("web origin must use HTTPS; HTTP is permitted only on loopback")
	}
	if strings.Contains(u.Host, "@") || strings.ContainsAny(u.Host, "\r\n\\") {
		return nil, errors.New("invalid web origin")
	}
	u.Path, u.RawPath = "", ""
	u.Host = strings.ToLower(u.Host)
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("invalid web origin port")
		}
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			host := u.Hostname()
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			u.Host = host
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("invalid web origin port")
	}
	return u, nil
}

func loopbackHost(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	if !s.validHost(r) {
		writeError(w, http.StatusForbidden, "invalid_host", "This host is not configured for Onibi.")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if s.assets == nil {
			http.NotFound(w, r)
			return
		}
		s.assets.ServeHTTP(w, r)
		return
	}
	if !s.validOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid_origin", "Use the configured Onibi address for this request.")
		return
	}
	if r.URL.Path == "/api/pair" {
		if requireMethod(w, r, http.MethodPost) {
			s.pair(w, r)
		}
		return
	}
	session, key, ok := s.authenticate(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Pair this browser with Onibi to continue.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Onibi-CSRF")), []byte(session.CSRF)) != 1 {
			writeError(w, http.StatusForbidden, "invalid_csrf", "Refresh Onibi before sending another action.")
			return
		}
	}
	switch r.URL.Path {
	case "/api/session":
		if requireMethod(w, r, http.MethodGet) {
			writeJSON(w, http.StatusOK, session)
		}
	case "/api/logout":
		if requireMethod(w, r, http.MethodPost) {
			s.mu.Lock()
			delete(s.sessions, key)
			s.mu.Unlock()
			s.setCookie(w, "", time.Unix(1, 0))
			writeJSON(w, http.StatusOK, map[string]bool{"loggedOut": true})
		}
	case "/api/targets":
		if requireMethod(w, r, http.MethodGet) {
			s.targets(w, r)
		}
	case "/api/snapshot":
		if requireMethod(w, r, http.MethodGet) {
			s.snapshot(w, r)
		}
	case "/api/actions":
		if requireMethod(w, r, http.MethodPost) {
			s.action(w, r)
		}
	default:
		if strings.HasPrefix(r.URL.Path, "/api/actions/") && requireMethod(w, r, http.MethodGet) {
			s.actionStatus(w, r, strings.TrimPrefix(r.URL.Path, "/api/actions/"))
		} else if !strings.HasPrefix(r.URL.Path, "/api/actions/") {
			writeError(w, http.StatusNotFound, "not_found", "Not found.")
		}
	}
}

func (s *Server) validHost(r *http.Request) bool {
	if r.Host == s.host {
		return true
	}
	if !s.proxyHosts[r.Host] {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && net.ParseIP(host).IsLoopback()
}

func (s *Server) validOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	value := r.Header.Get("Origin")
	if value != "" && value != s.origin {
		return false
	}
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) || value == s.origin
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	hash := sha256.Sum256([]byte(body.Token))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairUsed || !s.now().Before(s.pairExpires) || subtle.ConstantTimeCompare(hash[:], s.pairHash[:]) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_pairing", "This pairing link is invalid, expired, or already used. Restart Onibi locally for a new link.")
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "Could not create a browser session.")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "Could not create a browser session.")
		return
	}
	session := browserSession{CSRF: csrf, ExpiresAt: s.now().Add(sessionLifetime)}
	s.sessions[sha256.Sum256([]byte(token))] = session
	s.pairUsed, s.pairURL = true, ""
	s.setCookie(w, token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) authenticate(r *http.Request) (browserSession, [sha256.Size]byte, bool) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil || len(cookie.Value) != 43 {
		return browserSession{}, [sha256.Size]byte{}, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if ok && !s.now().Before(session.ExpiresAt) {
		delete(s.sessions, key)
		ok = false
	}
	return session, key, ok
}

func (s *Server) setCookie(w http.ResponseWriter, token string, expiry time.Time) {
	cookie := &http.Cookie{Name: s.cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, Expires: expiry}
	if token == "" {
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
}

func (s *Server) targets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	targets, err := s.backend.Targets(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "ghostty_unavailable", backendMessage(err, "Ghostty is unavailable. Check that it is running, supports Automation, and permits Onibi to control it."))
		return
	}
	if targets == nil {
		targets = []ghostty.Target{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets, "controls": ghostty.Controls(), "outputMode": "focused-text"})
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("targetId")
	if !validTargetID(id) {
		writeError(w, http.StatusBadRequest, "invalid_target", "An explicit target is required.")
		return
	}
	// Capture brackets the Accessibility read with two native identity checks.
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	snapshot, err := s.backend.Snapshot(ctx, id)
	if errors.Is(err, ghostty.ErrFocusRequired) {
		writeError(w, http.StatusConflict, "focus_required", "Focus this exact terminal to read its visible output.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable", backendMessage(err, "This terminal could not be read. Refresh the target list and check Ghostty permissions."))
		return
	}
	if snapshot.TargetID != id {
		writeError(w, http.StatusConflict, "target_changed", "The target changed during capture. Refresh the target list.")
		return
	}
	if len(snapshot.Text) > maxSnapshotBytes {
		snapshot.Text = snapshot.Text[len(snapshot.Text)-maxSnapshotBytes:]
		for len(snapshot.Text) > 0 && !utf8.RuneStart(snapshot.Text[0]) {
			snapshot.Text = snapshot.Text[1:]
		}
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type actionRequest struct {
	ID     string         `json:"id"`
	Action ghostty.Action `json:"action"`
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	var request actionRequest
	if !readJSON(w, r, &request) {
		return
	}
	if !validReceiptID(request.ID) {
		writeError(w, http.StatusBadRequest, "invalid_action_id", "Provide a unique action ID of 16 to 128 URL-safe characters.")
		return
	}
	if err := validateAction(request.Action); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_action", err.Error())
		return
	}
	raw, _ := json.Marshal(request.Action)
	hashBytes := sha256.Sum256(raw)
	hash := hex.EncodeToString(hashBytes[:])
	receipt, claimed, err := s.claimReceipt(r.Context(), request.ID, hash)
	if errors.Is(err, errReceiptConflict) {
		writeError(w, http.StatusConflict, "action_id_conflict", "This action ID was already used for different input.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The action could not be recorded. No input was sent.")
		return
	}
	if !claimed {
		writeJSON(w, http.StatusOK, receipt)
		return
	}
	// A disconnected browser must not interrupt result recording. The command
	// remains bounded, and a later GET can retrieve its durable receipt.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), actionTimeout)
	defer cancel()
	select {
	case s.execution <- struct{}{}:
		defer func() { <-s.execution }()
	case <-ctx.Done():
		receipt.State, receipt.Error = "rejected", "Onibi was busy. No input was sent."
		s.finishAction(w, receipt)
		return
	}
	if _, _, ok := s.authenticate(r); !ok {
		receipt.State, receipt.Error = "rejected", "The browser session ended before execution. No input was sent."
		s.finishAction(w, receipt)
		return
	}
	if err := s.validateTarget(ctx, request.Action); err != nil {
		receipt.State, receipt.Error = "rejected", err.Error()
		s.finishAction(w, receipt)
		return
	}
	result, err := s.backend.Execute(ctx, request.Action)
	if err != nil {
		if ghostty.IsRejected(err) {
			receipt.State, receipt.Error = "rejected", backendMessage(err, "Ghostty rejected the action before execution. Refresh the target list and check Automation permissions.")
		} else {
			receipt.State, receipt.Error = "uncertain", uncertainMessage
		}
	} else {
		receipt.State, receipt.Result = "completed", &result
	}
	s.finishAction(w, receipt)
}

func (s *Server) validateTarget(ctx context.Context, action ghostty.Action) error {
	targets, err := s.backend.Targets(ctx)
	if err != nil {
		return errors.New("Ghostty could not be checked before execution. No input was sent.")
	}
	if action.Kind == "new_window" {
		return nil
	}
	for _, target := range targets {
		switch action.Kind {
		case "new_tab", "close_window":
			if target.WindowID == action.WindowID {
				return nil
			}
		case "close_tab":
			if target.WindowID == action.WindowID && target.TabID == action.TabID && (action.TargetID == "" || target.ID == action.TargetID) {
				return nil
			}
		default:
			if target.ID == action.TargetID && (action.WindowID == "" || target.WindowID == action.WindowID) && (action.TabID == "" || target.TabID == action.TabID) {
				if (action.Kind == "control" || action.Kind == "rename_tab" || action.Kind == "rename_pane") && !target.Focused {
					return errors.New("Focus this pane on your Mac before using layout, scrollback, display, or title controls. No action was sent.")
				}
				return nil
			}
		}
	}
	return errors.New("The selected target is no longer available. No input was sent.")
}

func validateAction(action ghostty.Action) error {
	if len(action.Text) > maxInputBytes {
		return errors.New("Input must be at most 16 KiB.")
	}
	return ghostty.ValidateAction(action)
}

func validTargetID(id string) bool {
	return id != "" && len(id) <= 1024 && utf8.ValidString(id) && !strings.ContainsAny(id, "\x00\r\n")
}

// Only typed native rejections and known capability errors contain curated
// user-facing messages. Raw execution errors may include process diagnostics.
func backendMessage(err error, fallback string) string {
	if ghostty.IsRejected(err) || errors.Is(err, ghostty.ErrUnsupported) || errors.Is(err, ghostty.ErrCaptureUnavailable) {
		if len(err.Error()) <= 1024 {
			return err.Error()
		}
	}
	return fallback
}

func validReceiptID(id string) bool {
	if len(id) < 16 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	return false
}

func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Use application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err == nil {
		var extra any
		if err = decoder.Decode(&extra); errors.Is(err, io.EOF) {
			return true
		}
	}
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds 64 KiB.")
	} else {
		writeError(w, http.StatusBadRequest, "invalid_json", "Provide one valid JSON object with the documented fields.")
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}
