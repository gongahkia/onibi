package main

import (
    "net/http"
    "os/exec"
)

func execHandler(w http.ResponseWriter, r *http.Request) {
    cmd := r.FormValue("cmd")
    out, _ := exec.Command("sh", "-c", cmd).Output() // sink
    _, _ = w.Write(out)
}

func main() {
    http.HandleFunc("/run", execHandler)
}
