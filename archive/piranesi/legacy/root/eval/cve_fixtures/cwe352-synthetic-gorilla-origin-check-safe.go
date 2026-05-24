package main

import "net/http"

func checkSameOrigin(r *http.Request, allowedOrigin string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if r.TLS == nil { // fixed: rely on server TLS context, not URL.Scheme
		return true
	}
	return origin == allowedOrigin
}
