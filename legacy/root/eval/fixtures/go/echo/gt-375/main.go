package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
)

func main() {
    e := echo.New()
    e.GET("/fetch", func(c echo.Context) error {
        resp, _ := http.Get(c.QueryParam("url")) // sink
        _ = resp
        return c.NoContent(http.StatusOK)
    })
}
