package main

import "net/http"

func proxyHandler(w http.ResponseWriter, r *http.Request) {
    resp, _ := http.Get(r.URL.Query().Get("url")) // sink
    _ = resp
    w.WriteHeader(http.StatusOK)
}

func main() {
    http.HandleFunc("/proxy", proxyHandler)
}
