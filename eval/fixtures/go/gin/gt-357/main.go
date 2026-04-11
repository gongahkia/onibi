package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

var db *gorm.DB

func main() {
    r := gin.Default()
    r.GET("/users/raw", func(c *gin.Context) {
        rows, _ := db.Raw("SELECT * FROM users WHERE id = '" + c.Query("id") + "'").Rows() // sink
        _ = rows
        c.Status(http.StatusOK)
    })
}
