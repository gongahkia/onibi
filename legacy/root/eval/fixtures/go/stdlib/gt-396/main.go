package main

import (
    "html/template"
    "net/http"
)

func parseUserTemplate(raw string) *template.Template {
    tpl, _ := template.New("mail").Parse(raw) // sink
    return tpl
}

func inviteHandler(w http.ResponseWriter, r *http.Request) {
    tpl := parseUserTemplate(r.URL.Query().Get("tpl"))
    _ = tpl.Execute(w, nil)
}

func main() {
    http.HandleFunc("/invite", inviteHandler)
}
