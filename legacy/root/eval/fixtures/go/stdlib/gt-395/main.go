package main

import (
    "html/template"
    "net/http"
)

func renderHandler(w http.ResponseWriter, r *http.Request) {
    tpl, _ := template.New("page").Parse(r.FormValue("tpl")) // sink
    _ = tpl.Execute(w, nil)
}

func main() {
    http.HandleFunc("/render", renderHandler)
}
