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

func historyHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
}

func refundHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

func main() {
    r := chi.NewRouter()
    r.Route("/billing", func(protected chi.Router) {
        protected.Use(authMiddleware)
        protected.Get("/history", historyHandler)
    })
    r.Delete("/billing/refunds/{id}", refundHandler) // sink
}
