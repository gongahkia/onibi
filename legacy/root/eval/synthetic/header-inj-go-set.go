package main

func handler(w ResponseWriter, r Request) {
    w.Header().Set("X-Custom", r.URL.Query().Get("v"))
}
