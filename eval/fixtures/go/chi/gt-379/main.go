package main

import (
    "net/http"

    "github.com/go-chi/chi/v5"
)

func transferFunds(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

func main() {
    r := chi.NewRouter()
    r.Post("/payments/transfer", transferFunds) // sink
}
