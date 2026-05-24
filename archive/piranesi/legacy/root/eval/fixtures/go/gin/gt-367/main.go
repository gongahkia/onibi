package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()
    r.GET("/download/:path", func(c *gin.Context) {
        path := c.Param("path")
        c.File(path) // sink
    })
}
