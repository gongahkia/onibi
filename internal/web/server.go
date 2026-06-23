package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	DefaultAddr = ":8443"

	ptySubprotocol    = "onibi.pty.v1"
	eventsSubprotocol = "onibi.events.v1"
)

type Options struct {
	TLSCert  tls.Certificate
	DB       *store.DB
	PTYHosts func() map[string]*pty.Host
	Log      *slog.Logger
}

type Server struct {
	tlsCert  tls.Certificate
	db       *store.DB
	ptyHosts func() map[string]*pty.Host
	log      *slog.Logger
}

func New(opts Options) *Server {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Server{
		tlsCert:  opts.TLSCert,
		db:       opts.DB,
		ptyHosts: opts.PTYHosts,
		log:      opts.Log,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/pair/{token}", s.handlePair)
	mux.HandleFunc("/ws/pty", s.handleWSPTY)
	mux.HandleFunc("/ws/events", s.handleWSEvents)
	mux.HandleFunc("/control", s.handleControl)
	mux.HandleFunc("/", s.handleRoot)
	return mux
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
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><title>Onibi</title><body>Onibi web cockpit paired.</body>"))
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
