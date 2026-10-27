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
	"log/slog"
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
	"github.com/gongahkia/onibi/internal/prompt"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/webpush"
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
	Logger  *slog.Logger
	Backend Backend
	// Sessions is optional. When set, daemon-managed multiplexer sessions
	// appear alongside Ghostty targets and keep working without Ghostty.
	Sessions     SessionBackend
	DB           *store.DB
	PublicOrigin string
	Assets       fs.FS
	Diagnostics  func(context.Context) ghostty.Diagnostics
	// Now defaults to time.Now. A supplied clock must be safe for concurrent
	// calls; tests can advance expiry without changing session policy.
	Now func() time.Time
	// ProxyHosts are explicit loopback upstream authorities used by a local
	// HTTPS proxy. They are accepted only from a loopback peer. Forwarded
	// headers never determine the trusted public origin or cookie security.
	ProxyHosts []string
}

type browserSession struct {
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"expiresAt"`
	Device    Device    `json:"device"`
}

type Server struct {
	log                    *slog.Logger
	started                time.Time
	metrics                counters
	featureObservations    map[string]FeatureObservation
	backend                Backend
	managed                SessionBackend
	db                     *store.DB
	origin                 string
	host                   string
	proxyHosts             map[string]bool
	secure                 bool
	cookieName             string
	assets                 http.Handler
	execution              chan struct{}
	mu                     sync.Mutex
	pairHash               [sha256.Size]byte
	pairExpires            time.Time
	pairUsed               bool
	pairURL                string
	sessions               map[[sha256.Size]byte]browserSession
	now                    func() time.Time
	diagnostics            func(context.Context) ghostty.Diagnostics
	inventoryObservation   Observation
	captureObservation     Observation
	inventoryStarted       time.Time
	captureStarted         time.Time
	inventoryFlight        *inventoryFlight
	inventoryFailures      int
	inventoryRetryAt       time.Time
	inventoryErr           error
	closeObservationBudget time.Duration
	pushKey                webpush.Key
	pushSend               pushSender
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
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Server{
		log: opts.Logger, started: time.Now().UTC(),
		backend: opts.Backend, managed: opts.Sessions, db: opts.DB, diagnostics: opts.Diagnostics, origin: origin.String(), host: origin.Host,
		secure: origin.Scheme == "https", cookieName: "onibi_local_session",
		proxyHosts: map[string]bool{}, sessions: map[[sha256.Size]byte]browserSession{},
		execution: make(chan struct{}, 1), now: now,
		closeObservationBudget: 3 * time.Second,
		pairHash:               sha256.Sum256([]byte(token)), pairExpires: now().Add(pairingLifetime),
	}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
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
	if err := s.prepareDevices(ctx); err != nil {
		return nil, fmt.Errorf("prepare browser devices: %w", err)
	}
	if err := s.preparePush(ctx); err != nil {
		return nil, fmt.Errorf("prepare push notifications: %w", err)
	}
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

// ValidateOrigin checks configuration without making a network request.
func ValidateOrigin(raw string) error { _, err := parseOrigin(raw); return err }

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

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
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
	if r.URL.Path == "/api/session" && r.Method == http.MethodGet {
		s.resumeSession(w, r)
		return
	}
	session, _, err := s.checkSession(r)
	if errors.Is(err, errAuthUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "Onibi could not check this browser's access. Try again in a moment.")
		return
	}
	if err != nil {
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
	case "/api/capabilities":
		if requireMethod(w, r, http.MethodGet) {
			writeJSON(w, http.StatusOK, s.featureReport())
		}
	case "/api/health":
		if requireMethod(w, r, http.MethodGet) {
			s.health(w, r)
		}
	case "/api/session":
		if requireMethod(w, r, http.MethodGet) {
			writeJSON(w, http.StatusOK, session)
		}
	case "/api/logout":
		if requireMethod(w, r, http.MethodPost) {
			s.logout(w, r, session)
		}
	case "/api/devices":
		if requireMethod(w, r, http.MethodGet) {
			s.devices(w, r)
		}
	case "/api/devices/revoke":
		if requireMethod(w, r, http.MethodPost) {
			s.revokeDevice(w, r, session)
		}
	case "/api/push":
		if requireMethod(w, r, http.MethodGet) {
			s.pushStatus(w, r, session)
		}
	case "/api/push/subscribe":
		if requireMethod(w, r, http.MethodPost) {
			s.pushSubscribe(w, r, session)
		}
	case "/api/push/unsubscribe":
		if requireMethod(w, r, http.MethodPost) {
			s.pushUnsubscribe(w, r, session)
		}
	case "/api/push/test":
		if requireMethod(w, r, http.MethodPost) {
			s.pushTest(w, r, session)
		}
	case "/api/targets":
		if requireMethod(w, r, http.MethodGet) {
			s.targets(w, r)
		}
	case "/api/snapshot":
		if requireMethod(w, r, http.MethodGet) {
			s.snapshot(w, r)
		}
	case "/api/sessions":
		if requireMethod(w, r, http.MethodGet) {
			s.sessionList(w, r)
		}
	case "/api/sessions/snapshot":
		if requireMethod(w, r, http.MethodGet) {
			s.sessionSnapshot(w, r)
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
	targets, err := s.observedTargets(ctx)
	if err != nil {
		s.writeRetryAfter(w)
		writeError(w, http.StatusServiceUnavailable, ghostty.ErrorCode(err), backendMessage(err, "Ghostty is unavailable. Check that it is running, supports Automation, and permits Onibi to control it."))
		return
	}
	if targets == nil {
		targets = []ghostty.Target{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets, "controls": ghostty.Controls(), "outputMode": "focused-text", "capabilities": s.capabilities()})
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
	started := s.now()
	snapshot, err := s.backend.Snapshot(ctx, id)
	if err == nil && snapshot.TargetID != id {
		s.recordCapture(started, id, ghostty.ErrStaleTarget)
		writeError(w, http.StatusConflict, "target_changed", "The target changed during capture. Refresh the target list.")
		return
	}
	s.recordCapture(started, id, err)
	if errors.Is(err, ghostty.ErrFocusRequired) {
		writeError(w, http.StatusConflict, "focus_required", "Focus this exact terminal to read its visible output.")
		return
	}
	if err != nil {
		code := ghostty.ErrorCode(err)
		if code == "ghostty_unavailable" {
			code = "snapshot_unavailable"
		}
		writeError(w, http.StatusServiceUnavailable, code, backendMessage(err, "This terminal could not be read. Refresh the target list and check Ghostty permissions."))
		return
	}
	if len(snapshot.Text) > maxSnapshotBytes {
		snapshot.Text = snapshot.Text[len(snapshot.Text)-maxSnapshotBytes:]
		for len(snapshot.Text) > 0 && !utf8.RuneStart(snapshot.Text[0]) {
			snapshot.Text = snapshot.Text[1:]
		}
	}
	writeJSON(w, http.StatusOK, struct {
		ghostty.Snapshot
		Prompt *prompt.Prompt `json:"prompt,omitempty"`
	}{snapshot, detectPrompt(snapshot.Text)})
}

// actionRequest carries exactly one of Action (a Ghostty target) or Session
// (a daemon-managed multiplexer session).
type actionRequest struct {
	ID      string         `json:"id"`
	Action  ghostty.Action `json:"action"`
	Session *SessionAction `json:"session,omitempty"`
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
	var hash, kind string
	if request.Session != nil {
		if !emptyAction(request.Action) {
			writeError(w, http.StatusBadRequest, "invalid_action", "Send either a terminal action or a session action, not both.")
			return
		}
		if err := validateSessionAction(*request.Session); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_action", err.Error())
			return
		}
		hash, kind = sessionActionHash(*request.Session), "session_"+request.Session.Kind
	} else {
		if err := validateAction(request.Action); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_action", err.Error())
			return
		}
		raw, _ := json.Marshal(request.Action)
		hashBytes := sha256.Sum256(raw)
		hash, kind = hex.EncodeToString(hashBytes[:]), request.Action.Kind
	}
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
		s.finishAction(w, receipt, kind)
		return
	}
	if _, _, ok := s.authenticate(r.WithContext(ctx)); !ok {
		receipt.State, receipt.Error = "rejected", "The browser session ended before execution. No input was sent."
		s.finishAction(w, receipt, kind)
		return
	}
	if request.Session != nil {
		s.finishAction(w, s.executeSession(ctx, r, receipt, *request.Session), kind)
		return
	}
	if err := s.validateTarget(ctx, request.Action); err != nil {
		receipt.State, receipt.Error = "rejected", err.Error()
		s.finishAction(w, receipt, request.Action.Kind)
		return
	}
	// Native validation can take seconds. Recheck the grant again after it,
	// immediately before dispatch, so revocation during a slow inventory wins.
	if _, _, ok := s.authenticate(r.WithContext(ctx)); !ok {
		receipt.State, receipt.Error = "rejected", "Browser access ended during target validation. No input was sent."
		s.finishAction(w, receipt, request.Action.Kind)
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
		if ghostty.IsClose(request.Action.Kind) {
			s.observeClose(ctx, request.Action, &result)
		}
		receipt.State, receipt.Result = "completed", &result
	}
	s.recordFeature(request.Action, result, err)
	s.finishAction(w, receipt, request.Action.Kind)
}

func (s *Server) validateTarget(ctx context.Context, action ghostty.Action) error {
	started := s.now()
	targets, err := s.backend.Targets(ctx)
	s.recordInventory(started, err)
	if err != nil {
		return errors.New("Ghostty could not be checked before execution. No input was sent.")
	}
	capabilities := s.capabilities()
	if capabilities.Known && (!capabilities.Control || ((action.Kind == "rename_tab" || action.Kind == "rename_pane") && !capabilities.Rename)) {
		return ghostty.ErrUnsupportedAction
	}
	if action.Kind == "new_window" {
		return nil
	}
	if err := ghostty.CheckCloseScope(action, targets); err != nil {
		return err
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

func emptyAction(action ghostty.Action) bool {
	raw, _ := json.Marshal(action)
	zero, _ := json.Marshal(ghostty.Action{})
	return string(raw) == string(zero)
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
