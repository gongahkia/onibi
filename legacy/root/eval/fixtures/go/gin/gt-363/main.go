package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
    r.GET("/fetch", func(c *gin.Context) {
        resp, _ := http.Get(c.Query("url")) // sink
        _ = resp
        c.Status(http.StatusOK)
    })
}
