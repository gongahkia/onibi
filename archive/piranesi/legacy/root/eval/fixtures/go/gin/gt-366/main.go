package main

import "github.com/gin-gonic/gin"

func renderSnippet(input string) string {
    return "<section>" + input + "</section>"
}

func main() {
    r := gin.Default()
    r.GET("/snippet", func(c *gin.Context) {
        body := renderSnippet(c.Query("body"))
        _, _ = c.Writer.Write([]byte(body)) // sink
    })
}
