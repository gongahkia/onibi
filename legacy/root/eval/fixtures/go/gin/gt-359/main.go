package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()
    r.GET("/preview", func(c *gin.Context) {
        input := c.Query("input")
        _, _ = c.Writer.Write([]byte(input)) // sink
    })
}
