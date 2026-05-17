package main

import "net/http"

func previewHandler(w http.ResponseWriter, r *http.Request) {
    _, _ = w.Write([]byte(r.URL.Query().Get("html"))) // sink
}

func main() {
    http.HandleFunc("/preview", previewHandler)
}
