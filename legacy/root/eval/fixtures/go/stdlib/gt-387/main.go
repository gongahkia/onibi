package main

import (
    "database/sql"
    "net/http"
)

var db *sql.DB

func userHandler(w http.ResponseWriter, r *http.Request) {
    row := db.QueryRow("SELECT name FROM users WHERE id = '" + r.URL.Query().Get("id") + "'") // sink
    var name string
    _ = row.Scan(&name)
    w.WriteHeader(http.StatusOK)
}

func main() {
    http.HandleFunc("/user", userHandler)
}
