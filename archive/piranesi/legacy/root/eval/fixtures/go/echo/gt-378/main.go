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

func refundHandler(c echo.Context) error {
    return c.NoContent(http.StatusNoContent)
}

func main() {
    e := echo.New()
    billing := e.Group("/billing", authMiddleware)
    billing.GET("/history", func(c echo.Context) error {
        return c.NoContent(http.StatusOK)
    })
    e.DELETE("/billing/refunds/:id", refundHandler) // sink
}
