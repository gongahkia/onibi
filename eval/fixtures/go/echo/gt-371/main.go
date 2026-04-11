package main

import "github.com/labstack/echo/v4"

func main() {
    e := echo.New()
    e.GET("/preview", func(c echo.Context) error {
        input := c.QueryParam("input")
        _, _ = c.Response().Write([]byte(input)) // sink
        return nil
    })
}
