package main

import "net/http"

func fetch() {
    _, _ = http.Get("http://api.example.com/users")
}
