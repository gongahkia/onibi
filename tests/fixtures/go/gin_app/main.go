package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"

	"github.com/gin-gonic/gin"
)

var db *sql.DB

func main() {
	router := gin.Default()

	router.GET("/sql", func(c *gin.Context) {
		rows, _ := db.Query(fmt.Sprintf("SELECT * FROM users WHERE name = '%s'", c.Query("user")))
		_ = rows
		c.String(http.StatusOK, "ok")
	})

	router.GET("/cmd", func(c *gin.Context) {
		name := c.Query("cmd")
		command := exec.Command(name)
		_ = command
		c.String(http.StatusOK, "ok")
	})

	router.GET("/html", func(c *gin.Context) {
		markup := c.Query("markup")
		safe := template.HTML(markup)
		_ = safe
		c.String(http.StatusOK, "ok")
	})

	router.GET("/file", func(c *gin.Context) {
		filename := c.Query("file")
		handle, _ := os.Open(filename)
		_ = handle
		c.String(http.StatusOK, "ok")
	})
}
