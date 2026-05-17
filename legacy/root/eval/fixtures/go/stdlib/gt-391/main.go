package main

import "net/http"

func fileHandler(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, r.URL.Query().Get("path")) // sink
}

func main() {
    http.HandleFunc("/files", fileHandler)
}
