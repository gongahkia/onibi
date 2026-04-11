package main

import "github.com/labstack/echo/v4"

func main() {
    e := echo.New()
    e.GET("/archive/:path", func(c echo.Context) error {
        return c.File("/srv/archive/" + c.Param("path")) // sink
    })
}
