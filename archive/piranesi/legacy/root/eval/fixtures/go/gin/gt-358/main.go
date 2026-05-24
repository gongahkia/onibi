package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

var db *gorm.DB

func main() {
    r := gin.Default()
    r.GET("/orders/raw", func(c *gin.Context) {
        rows, _ := db.Raw("SELECT * FROM orders WHERE account_id = '" + c.Query("account") + "'").Rows() // sink
        _ = rows
        c.Status(http.StatusOK)
    })
}
