package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
)

func main() {
    e := echo.New()
    e.GET("/proxy", func(c echo.Context) error {
        req, _ := http.NewRequest(http.MethodGet, c.QueryParam("url"), nil) // sink
        resp, _ := http.DefaultClient.Do(req)
        _ = resp
        return c.NoContent(http.StatusOK)
    })
}
