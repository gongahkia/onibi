package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
    "gorm.io/gorm"
)

var db *gorm.DB

func main() {
    e := echo.New()
    e.GET("/users/raw", func(c echo.Context) error {
        rows, _ := db.Raw("SELECT * FROM users WHERE id = '" + c.QueryParam("id") + "'").Rows() // sink
        _ = rows
        return c.NoContent(http.StatusOK)
    })
}
