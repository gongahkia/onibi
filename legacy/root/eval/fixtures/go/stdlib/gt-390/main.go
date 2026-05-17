package main

import (
    "fmt"
    "net/http"
)

func searchHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "<p>%s</p>", r.URL.Query().Get("q")) // sink
}

func main() {
    http.HandleFunc("/search", searchHandler)
}
