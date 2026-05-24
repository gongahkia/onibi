package main

import (
    "net/http"
    "strings"

    "github.com/go-chi/chi/v5"
)

func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/admin/public") {
            next.ServeHTTP(w, r)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func exportHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

func main() {
    r := chi.NewRouter()
    r.Use(authMiddleware)
    r.Post("/admin/public/export", exportHandler) // sink
}
