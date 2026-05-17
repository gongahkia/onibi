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

func updateEmail(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

func main() {
    r := chi.NewRouter()
    r.Use(authMiddleware)
    r.Post("/profile/email", updateEmail) // sink
}
