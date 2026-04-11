package main

import "github.com/labstack/echo/v4"

func main() {
    e := echo.New()
    e.GET("/welcome", func(c echo.Context) error {
        name := c.QueryParam("name")
        _, _ = c.Response().Write([]byte("<h1>" + name + "</h1>")) // sink
        return nil
    })
}
