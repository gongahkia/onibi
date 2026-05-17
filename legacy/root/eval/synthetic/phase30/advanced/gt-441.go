package advanced

import (
    "net/http"
    "os"
)

func DeleteReport(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Query().Get("path")
    info, _ := os.Stat(path)
    if info.Mode().Perm()&0002 != 0 {
        os.Remove(path) // sink
    }
}
