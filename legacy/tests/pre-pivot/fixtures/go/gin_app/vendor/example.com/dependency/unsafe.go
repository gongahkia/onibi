package dependency

import (
	"os"

	"github.com/gin-gonic/gin"
)

func Ignored(c *gin.Context) {
	handle, _ := os.Open(c.Query("vendor"))
	_ = handle
}
