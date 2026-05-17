package main

import (
    "net/http"

    "github.com/go-chi/chi/v5"
)

func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        next.ServeHTTP(w, r)
    })
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
}

func main() {
    r := chi.NewRouter()
    r.Get("/admin/metrics", metricsHandler) // sink
    r.Use(authMiddleware)
}
