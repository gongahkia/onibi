package advanced

import (
    "net/http"
    "os"
)

func HandleUpload(w http.ResponseWriter, r *http.Request) {
    path := "/tmp/" + r.FormValue("name")
    if _, err := os.Stat(path); os.IsNotExist(err) {
        os.WriteFile(path, []byte(r.FormValue("body")), 0644) // sink
    }
}
