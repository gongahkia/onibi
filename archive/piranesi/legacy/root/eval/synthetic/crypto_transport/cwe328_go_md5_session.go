package main

import "crypto/md5"

func sessionToken(v string) [16]byte {
    return md5.Sum([]byte(v))
}
