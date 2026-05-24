package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

var db *gorm.DB

func buildUserQuery(id string) string {
    return "SELECT * FROM users WHERE id = '" + id + "'"
}

func main() {
    r := gin.Default()
    r.GET("/lookup", func(c *gin.Context) {
        rows, _ := db.Raw(buildUserQuery(c.Query("id"))).Rows() // sink
        _ = rows
        c.Status(http.StatusOK)
    })
}
