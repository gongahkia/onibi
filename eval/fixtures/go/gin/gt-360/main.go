package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()
    r.GET("/banner", func(c *gin.Context) {
        name := c.Query("name")
        _, _ = c.Writer.Write([]byte("<div>" + name + "</div>")) // sink
    })
}
