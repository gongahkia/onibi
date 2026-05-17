package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
)

func authMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        return next(c)
    }
}

func exportHandler(c echo.Context) error {
    return c.NoContent(http.StatusNoContent)
}

func dashboardHandler(c echo.Context) error {
    return c.NoContent(http.StatusOK)
}

func main() {
    e := echo.New()
    admin := e.Group("/admin", authMiddleware)
    admin.GET("/dashboard", dashboardHandler)
    e.POST("/admin/export", exportHandler) // sink
}
