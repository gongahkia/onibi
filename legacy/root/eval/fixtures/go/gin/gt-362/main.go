package main

import "github.com/gin-gonic/gin"

func main() {
    r := gin.Default()
    r.GET("/static/:path", func(c *gin.Context) {
        c.File("/srv/www/" + c.Param("path")) // sink
    })
}
