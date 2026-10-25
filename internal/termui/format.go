package termui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Elapsed(d time.Duration) string {
	seconds := max(0, int(d.Seconds()))
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
func ShortPath(path string) string {
	if cwd, err := os.Getwd(); err == nil && strings.HasPrefix(path, cwd+string(filepath.Separator)) {
		return "." + strings.TrimPrefix(path, cwd)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
