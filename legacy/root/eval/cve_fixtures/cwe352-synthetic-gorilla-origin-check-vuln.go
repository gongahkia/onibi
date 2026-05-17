package main

import "net/http"

func checkSameOrigin(r *http.Request, allowedOrigin string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if r.URL.Scheme == "" { // vulnerable: server-side requests commonly leave URL.Scheme empty
		return true
	}
	return origin == allowedOrigin
}
