package advanced

import "net/http"

func Callback(w http.ResponseWriter, r *http.Request) {
    next := r.URL.Query().Get("next")
    code := issueCode(r.URL.Query().Get("user"))
    http.Redirect(w, r, next+"?code="+code, http.StatusFound) // sink
}

func issueCode(user string) string {
    return "code-for-" + user
}
