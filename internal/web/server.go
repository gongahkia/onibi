package web

import (
	"context"
	"crypto/tls"
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
	PTYHosts      func() map[string]*pty.Host
	Log           *slog.Logger
}

type Server struct {
	tlsCert       tls.Certificate
	db            *store.DB
	approvalQueue *approval.Queue
	ptyHosts      func() map[string]*pty.Host
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
		ptyHosts:      opts.PTYHosts,
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
	mux.HandleFunc("/assets/", s.handleAssets)
	mux.HandleFunc("/control", s.handleControl)
	mux.HandleFunc("/approval/{id}", s.handleApproval)
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"version": buildinfo.Version,
	})
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
		http.Error(w, "forbidden", http.StatusForbidden)
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

func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	hosts := map[string]*pty.Host{}
	if s.ptyHosts != nil {
		if got := s.ptyHosts(); got != nil {
			hosts = got
		}
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID != "" {
		if hosts[sessionID] == nil {
			s.log.Warn("web session info failed", "request_id", requestID(r), "reason", "session_not_found", "requested_session", sessionID, "active_sessions", len(hosts))
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
	} else {
		if len(hosts) != 1 {
			s.log.Warn("web session info failed", "request_id", requestID(r), "reason", "active_session_count_not_one", "active_sessions", len(hosts))
			http.Error(w, "exactly one active session required", http.StatusConflict)
			return
		}
		for id := range hosts {
			sessionID = id
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID,
		"ws_token":   ownerSessionID,
	})
}

func (s *Server) hostForSession(sessionID string) (*pty.Host, bool) {
	if s.ptyHosts == nil {
		return nil, false
	}
	hosts := s.ptyHosts()
	if hosts == nil {
		return nil, false
	}
	h := hosts[sessionID]
	return h, h != nil
}
