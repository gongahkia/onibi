// Package api exposes the local application service over a bounded HTTP API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/kaypoh/internal/app"
	"github.com/gongahkia/kaypoh/internal/config"
	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/store"
)

const maxRequestBody = 1 << 20

type Server struct {
	service     *app.Service
	config      config.API
	bearerToken string
	httpServer  *http.Server
}

func New(service *app.Service, cfg config.Config) (*Server, error) {
	if service == nil {
		return nil, errors.New("API service is required")
	}
	host, _, err := net.SplitHostPort(cfg.API.Address)
	if err != nil {
		return nil, fmt.Errorf("parse API address: %w", err)
	}
	if !cfg.API.AllowRemote && !isLoopbackHost(host) {
		return nil, errors.New("API address must be loopback unless api.allow_remote is true")
	}
	token := ""
	if cfg.API.AllowRemote {
		token, err = cfg.ResolveSecret(cfg.API.AuthToken)
		if err != nil || strings.TrimSpace(token) == "" {
			return nil, errors.New("remote API bearer token is unavailable")
		}
	}
	server := &Server{service: service, config: cfg.API, bearerToken: token}
	server.httpServer = &http.Server{Addr: cfg.API.Address, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	return server, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", server.health)
	mux.HandleFunc("GET /v1/sources", server.sources)
	mux.HandleFunc("GET /v1/venues", server.venues)
	mux.HandleFunc("POST /v1/search", server.search)
	mux.HandleFunc("GET /v1/watches", server.watches)
	mux.HandleFunc("POST /v1/watches", server.createWatch)
	mux.HandleFunc("POST /v1/watches/evaluate", server.evaluateWatches)
	mux.HandleFunc("GET /v1/events", server.events)
	mux.HandleFunc("GET /v1/deliveries", server.deliveries)
	return server.authorize(mux)
}

func (server *Server) Serve() error {
	err := server.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (server *Server) Shutdown(ctx context.Context) error {
	return server.httpServer.Shutdown(ctx)
}

func (server *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if server.bearerToken != "" && request.Header.Get("Authorization") != "Bearer "+server.bearerToken {
			writeError(writer, http.StatusUnauthorized, "bearer token required")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) health(writer http.ResponseWriter, request *http.Request) {
	report, err := server.service.Doctor(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, report)
}

func (server *Server) sources(writer http.ResponseWriter, request *http.Request) {
	result, err := server.service.Sources(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) venues(writer http.ResponseWriter, request *http.Request) {
	limit, err := queryLimit(request, 100, 500)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	result, err := server.service.Venues(request.Context(), store.VenueFilter{Search: request.URL.Query().Get("search"), Limit: limit})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) search(writer http.ResponseWriter, request *http.Request) {
	var query domain.Query
	if err := decodeJSON(writer, request, &query); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	result, err := server.service.Search(request.Context(), query)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) watches(writer http.ResponseWriter, request *http.Request) {
	enabledOnly := request.URL.Query().Get("enabled") == "true"
	result, err := server.service.Watches(request.Context(), enabledOnly)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) createWatch(writer http.ResponseWriter, request *http.Request) {
	var watch domain.Watch
	if err := decodeJSON(writer, request, &watch); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	result, err := server.service.CreateWatch(request.Context(), watch)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (server *Server) evaluateWatches(writer http.ResponseWriter, request *http.Request) {
	result, err := server.service.EvaluateWatches(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) events(writer http.ResponseWriter, request *http.Request) {
	limit, err := queryLimit(request, 100, 1_000)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	result, err := server.service.Events(request.Context(), request.URL.Query().Get("watch"), limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) deliveries(writer http.ResponseWriter, request *http.Request) {
	limit, err := queryLimit(request, 100, 1_000)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	result, err := server.service.Deliveries(request.Context(), request.URL.Query().Get("event"), limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, value any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func queryLimit(request *http.Request, fallback, maximum int) (int, error) {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
