package main

import (
    "net/http"
    "os"
)

func documentHandler(w http.ResponseWriter, r *http.Request) {
    data, _ := os.ReadFile("/srv/docs/" + r.URL.Query().Get("name")) // sink
    _, _ = w.Write(data)
}

func main() {
    http.HandleFunc("/docs", documentHandler)
}
