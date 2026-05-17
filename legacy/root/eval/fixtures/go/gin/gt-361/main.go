package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()
    r.GET("/files/:path", func(c *gin.Context) {
        c.File(c.Param("path")) // sink
    })
}
