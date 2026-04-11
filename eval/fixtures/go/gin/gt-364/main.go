package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
    r.GET("/proxy", func(c *gin.Context) {
        req, _ := http.NewRequest(http.MethodGet, c.Query("url"), nil) // sink
        resp, _ := http.DefaultClient.Do(req)
        _ = resp
        c.Status(http.StatusOK)
    })
}
