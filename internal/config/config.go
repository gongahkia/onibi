package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Server struct {
	Command  []string `json:"command"`
	Language string   `json:"language"`
}
type Config struct {
	TabSize         int               `json:"tab_size"`
	InsertSpaces    bool              `json:"insert_spaces"`
	AutoIndent      bool              `json:"auto_indent"`
	Mouse           bool              `json:"mouse"`
	Syntax          bool              `json:"syntax"`
	LSP             bool              `json:"lsp"`
	Theme           string            `json:"theme"`
	ServiceMaxBytes int               `json:"service_max_bytes"`
	MaxFileMiB      int               `json:"max_file_mib"`
	Servers         map[string]Server `json:"servers"`
}

func Default() Config {
	return Config{TabSize: 4, InsertSpaces: true, AutoIndent: true, Mouse: true, Syntax: true, LSP: true, Theme: "dark", ServiceMaxBytes: 1 << 20, MaxFileMiB: 256, Servers: map[string]Server{}}
}
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "shack", "config.json")
}
func Load(path string, explicit bool) (Config, error) {
	c := Default()
	f, err := os.Open(path)
	if os.IsNotExist(err) && !explicit {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	if c.TabSize < 1 || c.TabSize > 16 || c.ServiceMaxBytes < 0 || c.ServiceMaxBytes > 16<<20 || c.MaxFileMiB < 1 || c.MaxFileMiB > 1024 {
		return c, fmt.Errorf("config: tab_size must be 1–16, service_max_bytes 0–16777216, max_file_mib 1–1024")
	}
	if c.Theme != "dark" && c.Theme != "light" {
		return c, fmt.Errorf("config: theme must be dark or light")
	}
	return c, nil
}
