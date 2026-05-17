package main

import "crypto/rsa"
import "crypto/rand"

func weak() {
    _, _ = rsa.GenerateKey(rand.Reader, 1024)
}
