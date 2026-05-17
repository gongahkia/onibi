package main

import (
    "net/http"

    "github.com/go-chi/chi/v5"
)

func rotateKeys(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

func adminRoutes() http.Handler {
    r := chi.NewRouter()
    r.Post("/keys/rotate", rotateKeys)
    return r
}

func main() {
    r := chi.NewRouter()
    r.Mount("/admin", adminRoutes()) // sink
}
