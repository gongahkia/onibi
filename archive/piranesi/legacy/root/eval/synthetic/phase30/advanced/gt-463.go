package advanced

import (
    "net/http"
    "os"
)

func CheckAPIKey(w http.ResponseWriter, r *http.Request) {
    if r.Header.Get("X-API-Key") == os.Getenv("INTERNAL_KEY") { // sink
        w.Write([]byte("ok"))
        return
    }
    http.Error(w, "unauthorized", http.StatusUnauthorized)
}
