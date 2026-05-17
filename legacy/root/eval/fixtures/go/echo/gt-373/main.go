package main

import "github.com/labstack/echo/v4"

func main() {
    e := echo.New()
    e.GET("/files/:path", func(c echo.Context) error {
        return c.File(c.Param("path")) // sink
    })
}
