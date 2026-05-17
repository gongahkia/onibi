package main

import "github.com/golang-jwt/jwt/v5"

func parse(tokenString string) {
    _, _ = jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
        if token.Method != jwt.SigningMethodHS256 {
            return nil, jwt.ErrTokenSignatureInvalid
        }
        return []byte("secret"), nil
    })
}
