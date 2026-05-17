package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
    "gorm.io/gorm"
)

var db *gorm.DB

func buildOrderQuery(id string) string {
    return "SELECT * FROM orders WHERE owner_id = '" + id + "'"
}

func main() {
    e := echo.New()
    e.GET("/orders", func(c echo.Context) error {
        rows, _ := db.Raw(buildOrderQuery(c.QueryParam("id"))).Rows() // sink
        _ = rows
        return c.NoContent(http.StatusOK)
    })
}
