package main

import "math/rand"

func issue() int {
    sessionToken := rand.Intn(1000000)
    return sessionToken
}
