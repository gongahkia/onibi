package main

import "crypto/rc4"

func encrypt(secret []byte) {
    _, _ = rc4.NewCipher(secret)
}
