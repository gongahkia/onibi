package main

import (
    "database/sql"
    "net/http"
)

var db *sql.DB

func deleteHandler(w http.ResponseWriter, r *http.Request) {
    _, _ = db.Exec("DELETE FROM sessions WHERE id = " + r.FormValue("id")) // sink
    w.WriteHeader(http.StatusNoContent)
}

func main() {
    http.HandleFunc("/sessions/delete", deleteHandler)
}
