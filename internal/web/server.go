package web

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
	webstatic "github.com/gongahkia/onibi/internal/web/static"
)

const (
	DefaultAddr = ":8443"

	ptySubprotocol    = "onibi.pty.v1"
	eventsSubprotocol = "onibi.events.v1"
)

type Options struct {
	TLSCert       tls.Certificate
	DB            *store.DB
	ApprovalQueue *approval.Queue
	EventBus      *EventBus
	PTYHosts      func() map[string]*pty.Host
	SessionIDs    func() []string
	PTYHost       func(context.Context, string) (*pty.Host, error)
	Handover      func(context.Context, string, string) (string, error)
	Scroll        func(context.Context, string, string) error
	TrustRuntime  func(context.Context, TrustRuntimeRequest) (string, error)
	RelayKeys     *RelayKeys
	RequireE2E    bool
	Log           *slog.Logger
}

type Server struct {
	tlsCert       tls.Certificate
	db            *store.DB
	approvalQueue *approval.Queue
	eventBus      *EventBus
	ptyHosts      func() map[string]*pty.Host
	sessionIDs    func() []string
	ptyHost       func(context.Context, string) (*pty.Host, error)
	handover      func(context.Context, string, string) (string, error)
	scroll        func(context.Context, string, string) error
	trustRuntime  func(context.Context, TrustRuntimeRequest) (string, error)
	relayKeys     *RelayKeys
	requireE2E    bool
	log           *slog.Logger
}

func New(opts Options) *Server {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Server{
		tlsCert:       opts.TLSCert,
		db:            opts.DB,
		approvalQueue: opts.ApprovalQueue,
		eventBus:      opts.EventBus,
		ptyHosts:      opts.PTYHosts,
		sessionIDs:    opts.SessionIDs,
		ptyHost:       opts.PTYHost,
		handover:      opts.Handover,
		scroll:        opts.Scroll,
		trustRuntime:  opts.TrustRuntime,
		relayKeys:     opts.RelayKeys,
		requireE2E:    opts.RequireE2E,
		log:           opts.Log,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/pair/{token}", s.handlePair)
	mux.HandleFunc("/ws/pty", s.handleWSPTY)
	mux.HandleFunc("/ws/events", s.handleWSEvents)
	mux.HandleFunc("/session-info", s.handleSessionInfo)
	mux.HandleFunc("/manifest.webmanifest", s.handleStaticFile("dist/manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("/sw.js", s.handleStaticFile("dist/sw.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/icons/", s.handleIcons)
	mux.HandleFunc("/assets/", s.handleAssets)
	mux.HandleFunc("/control", s.handleControl)
	mux.HandleFunc("/handover", s.handleHandover)
	mux.HandleFunc("/approval/{id}", s.handleApproval)
	mux.HandleFunc("/trust/runtime", s.handleTrustRuntime)
	mux.HandleFunc("/", s.handleRoot)
	return s.loggedHandler(mux)
}

func (s *Server) Start(addr string) error {
	return s.StartContext(context.Background(), addr)
}

func (s *Server) StartContext(ctx context.Context, addr string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{s.tlsCert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(slogWriter{s}, "", 0),
	}
	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		errCh <- srv.Shutdown(shutdownCtx)
	}()
	err := srv.ListenAndServeTLS("", "")
	if errors.Is(err, http.ErrServerClosed) {
		return <-errCh
	}
	return err
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	resp := healthzResponse{
		OK:      true,
		Version: buildinfo.Version,
	}
	if verifierHex, ok, err := s.healthzKeyVerifierHex(r); err != nil {
		s.log.Warn("web health e2e verifier failed", "request_id", requestID(r), "err", err)
		http.Error(w, "health verifier unavailable", http.StatusInternalServerError)
		return
	} else if ok {
		resp.E2E = true
		resp.KeyVerifierHex = verifierHex
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

type healthzResponse struct {
	OK             bool   `json:"ok"`
	Version        string `json:"version"`
	E2E            bool   `json:"e2e"`
	KeyVerifierHex string `json:"key_verifier_hex,omitempty"`
}

func (s *Server) healthzKeyVerifierHex(r *http.Request) (string, bool, error) {
	if s.db == nil {
		return "", false, nil
	}
	cookie, err := r.Cookie(OwnerCookieName)
	if err != nil || cookie.Value == "" {
		return "", false, nil
	}
	ok, err := s.db.TouchWebSession(r.Context(), cookie.Value, time.Now())
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	verifier, ok, err := s.db.WebSessionKeyVerifier(r.Context(), cookie.Value)
	if err != nil || !ok {
		return "", false, err
	}
	return hex.EncodeToString(verifier), true, nil
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.authenticate(r); err != nil {
		s.log.Warn("web root auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "remote", remoteHost(r.RemoteAddr))
		writeRootForbidden(w, err)
		return
	}
	index, err := webstatic.FS.ReadFile("dist/index.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Onibi</title><body>Onibi web cockpit paired.</body>"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}

func writeRootForbidden(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	if errors.Is(err, errAuthMissingCookie) || errors.Is(err, errAuthEmptyCookie) {
		_, _ = w.Write([]byte(`<!doctype html><title>Onibi Forbidden</title><body><h1>Forbidden</h1><p>Owner cookie is missing. If this happened immediately after pairing, iOS likely did not trust Onibi's local HTTPS certificate.</p><p>Install the Onibi local CA profile printed by <code>onibi up</code>, enable full trust in iOS Certificate Trust Settings, then restart <code>onibi up</code> and scan the new QR.</p><p>Use a phone hotspot only when the phone cannot reach the pair URL at all.</p></body>`))
		return
	}
	_, _ = w.Write([]byte("forbidden"))
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.authenticate(r); err != nil {
		s.log.Warn("web asset auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "remote", remoteHost(r.RemoteAddr), "path", r.URL.Path)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	assets, err := fs.Sub(webstatic.FS, "dist/assets")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.StripPrefix("/assets/", http.FileServer(http.FS(assets))).ServeHTTP(w, r)
}

func (s *Server) handleIcons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.authenticate(r); err != nil {
		s.log.Warn("web icon auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "remote", remoteHost(r.RemoteAddr), "path", r.URL.Path)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	icons, err := fs.Sub(webstatic.FS, "dist/icons")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.StripPrefix("/icons/", http.FileServer(http.FS(icons))).ServeHTTP(w, r)
}

func (s *Server) handleStaticFile(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if _, err := s.authenticate(r); err != nil {
			s.log.Warn("web static auth failed", "request_id", requestID(r), "reason", err.Error(), "cookie_present", ownerCookiePresent(r), "remote", remoteHost(r.RemoteAddr), "path", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		body, err := webstatic.FS.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", contentType)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}
}

func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	sessionIDs := s.activeSessionIDs()
	sessionID := r.URL.Query().Get("session_id")
	if sessionID != "" {
		if !containsSessionID(sessionIDs, sessionID) {
			s.log.Warn("web session info failed", "request_id", requestID(r), "reason", "session_not_found", "requested_session", sessionID, "active_sessions", len(sessionIDs))
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
	} else {
		if len(sessionIDs) != 1 {
			s.log.Warn("web session info failed", "request_id", requestID(r), "reason", "active_session_count_not_one", "active_sessions", len(sessionIDs))
			http.Error(w, "exactly one active session required", http.StatusConflict)
			return
		}
		sessionID = sessionIDs[0]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID,
		"ws_token":   ownerSessionID,
	})
}

func (s *Server) activeSessionIDs() []string {
	if s.sessionIDs != nil {
		return uniqueSessionIDs(s.sessionIDs())
	}
	hosts := s.staticPTYHosts()
	ids := make([]string, 0, len(hosts))
	for id := range hosts {
		ids = append(ids, id)
	}
	return uniqueSessionIDs(ids)
}

func (s *Server) staticPTYHosts() map[string]*pty.Host {
	if s.ptyHosts == nil {
		return nil
	}
	hosts := s.ptyHosts()
	if hosts == nil {
		return nil
	}
	return hosts
}

func (s *Server) hostForSession(ctx context.Context, sessionID string) (*pty.Host, bool) {
	if s.ptyHost != nil {
		h, err := s.ptyHost(ctx, sessionID)
		if err == nil && h != nil {
			return h, true
		}
		if err != nil {
			s.log.Warn("web pty host resolver failed", "session_id", sessionID, "err", err)
		}
	}
	h := s.staticPTYHosts()[sessionID]
	return h, h != nil
}

func uniqueSessionIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func containsSessionID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
