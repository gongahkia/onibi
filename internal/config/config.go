package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Server struct {
	Command  []string `json:"command"`
	Language string   `json:"language"`
}
type Config struct {
	Tasks            map[string][]string `json:"tasks"`
	Recovery         bool                `json:"recovery"`
	RecoveryMaxBytes int                 `json:"recovery_max_bytes"`
	SoftWrap         bool                `json:"soft_wrap"`
	Keybindings      map[string]string   `json:"keybindings"`
	TabSize          int                 `json:"tab_size"`
	InsertSpaces     bool                `json:"insert_spaces"`
	AutoIndent       bool                `json:"auto_indent"`
	Mouse            bool                `json:"mouse"`
	Syntax           bool                `json:"syntax"`
	LSP              bool                `json:"lsp"`
	Theme            string              `json:"theme"`
	ServiceMaxBytes  int                 `json:"service_max_bytes"`
	MaxFileMiB       int                 `json:"max_file_mib"`
	Servers          map[string]Server   `json:"servers"`
}

func Default() Config {
	return Config{Tasks: map[string][]string{}, Recovery: true, RecoveryMaxBytes: 8 << 20, Keybindings: map[string]string{}, TabSize: 4, InsertSpaces: true, AutoIndent: true, Mouse: true, Syntax: true, LSP: true, Theme: "dark", ServiceMaxBytes: 1 << 20, MaxFileMiB: 256, Servers: map[string]Server{}}
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
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("config: expected one JSON object")
	}
	if c.RecoveryMaxBytes < 1024 || c.RecoveryMaxBytes > 64<<20 {
		return c, fmt.Errorf("recovery_max_bytes must be between 1024 and 67108864")
	}
	if c.TabSize < 1 || c.TabSize > 16 || c.ServiceMaxBytes < 0 || c.ServiceMaxBytes > 16<<20 || c.MaxFileMiB < 1 || c.MaxFileMiB > 1024 {
		return c, fmt.Errorf("config: tab_size must be 1–16, service_max_bytes 0–16777216, max_file_mib 1–1024")
	}
	for name, args := range c.Tasks {
		if name == "" || len(args) == 0 || len(args) > 64 || args[0] == "" {
			return c, fmt.Errorf("task %q needs 1–64 command arguments", name)
		}
	}
	if c.Theme != "dark" && c.Theme != "light" {
		return c, fmt.Errorf("config: theme must be dark or light")
	}
	return c, nil
}
