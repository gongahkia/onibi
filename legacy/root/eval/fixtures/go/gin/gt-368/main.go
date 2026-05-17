package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

func fetchURL(url string) {
    resp, _ := http.Get(url) // sink
    _ = resp
}

func main() {
    r := gin.Default()
    r.GET("/mirror", func(c *gin.Context) {
        fetchURL(c.Query("url"))
        c.Status(http.StatusOK)
    })
}
