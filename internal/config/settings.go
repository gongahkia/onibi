package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) Std() time.Duration           { return time.Duration(d) }
func (d Duration) String() string               { return time.Duration(d).String() }
func (d Duration) MarshalYAML() (any, error)    { return d.String(), nil }
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return errors.New("duration must be scalar")
	}
	v, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

type Config struct {
	Daemon      Daemon      `yaml:"daemon" json:"daemon"`
	Shell       Shell       `yaml:"shell" json:"shell"`
	Multiplexer Multiplexer `yaml:"multiplexer" json:"multiplexer"`
}

type Daemon struct {
	LivenessInterval Duration `yaml:"liveness_interval" json:"liveness_interval"`
}

type Shell struct {
	Default string `yaml:"default" json:"default"`
	Login   bool   `yaml:"login" json:"login"`
}

// Multiplexer selects the backend used for terminal-backed sessions. A project
// may override these values in <cwd>/.onibi.yaml without changing the daemon's
// global configuration.
type Multiplexer struct {
	Default string     `yaml:"default" json:"default"`
	Tmux    MuxBackend `yaml:"tmux" json:"tmux"`
	Zellij  MuxBackend `yaml:"zellij" json:"zellij"`
	Screen  MuxBackend `yaml:"screen" json:"screen"`
}

type MuxBackend struct {
	Bin    string `yaml:"bin" json:"bin"`
	Config string `yaml:"config" json:"config"`
}

type LoadMeta struct {
	Path     string
	Exists   bool
	Explicit map[string]bool
}

type KeyInfo struct {
	Key         string
	Default     string
	Current     string
	Explicit    bool
	Description string
}

func Default() Config {
	return Config{
		Daemon: Daemon{LivenessInterval: Duration(5 * time.Second)},
		Shell:  Shell{Default: "auto", Login: true},
		Multiplexer: Multiplexer{
			Default: "auto",
			Tmux:    MuxBackend{Config: "auto"},
			Zellij:  MuxBackend{Config: "auto"},
			Screen:  MuxBackend{Config: "auto"},
		},
	}
}

func Load(paths Paths) (Config, LoadMeta, error) {
	path := paths.Config
	if path == "" {
		path = filepath.Join(paths.StateDir, "config.yaml")
	}
	cfg := Default()
	meta := LoadMeta{Path: path, Explicit: map[string]bool{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, meta, nil
	}
	if err != nil {
		return cfg, meta, err
	}
	meta.Exists = true
	return loadBytes(path, b, cfg, meta)
}

func loadBytes(path string, b []byte, cfg Config, meta LoadMeta) (Config, LoadMeta, error) {
	var raw struct {
		Daemon struct {
			LivenessInterval *Duration `yaml:"liveness_interval"`
		} `yaml:"daemon"`
		Shell struct {
			Default *string `yaml:"default"`
			Login   *bool   `yaml:"login"`
		} `yaml:"shell"`
		Multiplexer struct {
			Default *string `yaml:"default"`
			Tmux    struct {
				Bin    *string `yaml:"bin"`
				Config *string `yaml:"config"`
			} `yaml:"tmux"`
			Zellij struct {
				Bin    *string `yaml:"bin"`
				Config *string `yaml:"config"`
			} `yaml:"zellij"`
			Screen struct {
				Bin    *string `yaml:"bin"`
				Config *string `yaml:"config"`
			} `yaml:"screen"`
		} `yaml:"multiplexer"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&raw); err != nil {
		return cfg, meta, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw.Daemon.LivenessInterval != nil {
		cfg.Daemon.LivenessInterval = *raw.Daemon.LivenessInterval
		meta.Explicit["daemon.liveness_interval"] = true
	}
	if raw.Shell.Default != nil {
		cfg.Shell.Default = strings.TrimSpace(*raw.Shell.Default)
		meta.Explicit["shell.default"] = true
	}
	if raw.Shell.Login != nil {
		cfg.Shell.Login = *raw.Shell.Login
		meta.Explicit["shell.login"] = true
	}
	if raw.Multiplexer.Default != nil {
		cfg.Multiplexer.Default = strings.ToLower(strings.TrimSpace(*raw.Multiplexer.Default))
		meta.Explicit["multiplexer.default"] = true
	}
	if raw.Multiplexer.Tmux.Bin != nil {
		cfg.Multiplexer.Tmux.Bin = strings.TrimSpace(*raw.Multiplexer.Tmux.Bin)
		meta.Explicit["multiplexer.tmux.bin"] = true
	}
	if raw.Multiplexer.Tmux.Config != nil {
		cfg.Multiplexer.Tmux.Config = strings.TrimSpace(*raw.Multiplexer.Tmux.Config)
		meta.Explicit["multiplexer.tmux.config"] = true
	}
	if raw.Multiplexer.Zellij.Bin != nil {
		cfg.Multiplexer.Zellij.Bin = strings.TrimSpace(*raw.Multiplexer.Zellij.Bin)
		meta.Explicit["multiplexer.zellij.bin"] = true
	}
	if raw.Multiplexer.Zellij.Config != nil {
		cfg.Multiplexer.Zellij.Config = strings.TrimSpace(*raw.Multiplexer.Zellij.Config)
		meta.Explicit["multiplexer.zellij.config"] = true
	}
	if raw.Multiplexer.Screen.Bin != nil {
		cfg.Multiplexer.Screen.Bin = strings.TrimSpace(*raw.Multiplexer.Screen.Bin)
		meta.Explicit["multiplexer.screen.bin"] = true
	}
	if raw.Multiplexer.Screen.Config != nil {
		cfg.Multiplexer.Screen.Config = strings.TrimSpace(*raw.Multiplexer.Screen.Config)
		meta.Explicit["multiplexer.screen.config"] = true
	}
	if err := cfg.Validate(); err != nil {
		return cfg, meta, fmt.Errorf("validate %s: %w", path, err)
	}
	return cfg, meta, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config path required")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) Validate() error {
	if c.Daemon.LivenessInterval.Std() < time.Second || c.Daemon.LivenessInterval.Std() > 5*time.Minute {
		return errors.New("daemon.liveness_interval must be between 1s and 5m")
	}
	if strings.TrimSpace(c.Shell.Default) == "" {
		return errors.New("shell.default required")
	}
	switch strings.ToLower(strings.TrimSpace(c.Multiplexer.Default)) {
	case "auto", "tmux", "zellij", "screen":
	default:
		return errors.New("multiplexer.default must be auto, tmux, zellij, or screen")
	}
	return nil
}

func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("duration required")
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("duration %q must be Go duration syntax or integer seconds", s)
	}
	return time.Duration(n) * time.Second, nil
}

func Set(cfg *Config, key, value string) error {
	switch strings.TrimSpace(key) {
	case "daemon.liveness_interval":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.LivenessInterval = Duration(d)
	case "shell.default":
		cfg.Shell.Default = strings.TrimSpace(value)
	case "shell.login":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Shell.Login = v
	case "multiplexer.default":
		cfg.Multiplexer.Default = strings.ToLower(strings.TrimSpace(value))
	case "multiplexer.tmux.bin":
		cfg.Multiplexer.Tmux.Bin = strings.TrimSpace(value)
	case "multiplexer.tmux.config":
		cfg.Multiplexer.Tmux.Config = strings.TrimSpace(value)
	case "multiplexer.zellij.bin":
		cfg.Multiplexer.Zellij.Bin = strings.TrimSpace(value)
	case "multiplexer.zellij.config":
		cfg.Multiplexer.Zellij.Config = strings.TrimSpace(value)
	case "multiplexer.screen.bin":
		cfg.Multiplexer.Screen.Bin = strings.TrimSpace(value)
	case "multiplexer.screen.config":
		cfg.Multiplexer.Screen.Config = strings.TrimSpace(value)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return cfg.Validate()
}

func Get(cfg Config, key string) (string, error) {
	switch strings.TrimSpace(key) {
	case "daemon.liveness_interval":
		return cfg.Daemon.LivenessInterval.String(), nil
	case "shell.default":
		return cfg.Shell.Default, nil
	case "shell.login":
		return strconv.FormatBool(cfg.Shell.Login), nil
	case "multiplexer.default":
		return cfg.Multiplexer.Default, nil
	case "multiplexer.tmux.bin":
		return cfg.Multiplexer.Tmux.Bin, nil
	case "multiplexer.tmux.config":
		return cfg.Multiplexer.Tmux.Config, nil
	case "multiplexer.zellij.bin":
		return cfg.Multiplexer.Zellij.Bin, nil
	case "multiplexer.zellij.config":
		return cfg.Multiplexer.Zellij.Config, nil
	case "multiplexer.screen.bin":
		return cfg.Multiplexer.Screen.Bin, nil
	case "multiplexer.screen.config":
		return cfg.Multiplexer.Screen.Config, nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func Keys(cfg Config, meta LoadMeta) []KeyInfo {
	def := Default()
	return []KeyInfo{
		{"daemon.liveness_interval", def.Daemon.LivenessInterval.String(), cfg.Daemon.LivenessInterval.String(), meta.Explicit["daemon.liveness_interval"], "managed tmux liveness polling cadence"},
		{"shell.default", def.Shell.Default, cfg.Shell.Default, meta.Explicit["shell.default"], "default shell for new tmux sessions"},
		{"shell.login", strconv.FormatBool(def.Shell.Login), strconv.FormatBool(cfg.Shell.Login), meta.Explicit["shell.login"], "run default shell as a login shell"},
		{"multiplexer.default", def.Multiplexer.Default, cfg.Multiplexer.Default, meta.Explicit["multiplexer.default"], "terminal multiplexer selected for new sessions (auto, tmux, zellij, screen)"},
		{"multiplexer.tmux.bin", def.Multiplexer.Tmux.Bin, cfg.Multiplexer.Tmux.Bin, meta.Explicit["multiplexer.tmux.bin"], "tmux executable override"},
		{"multiplexer.tmux.config", def.Multiplexer.Tmux.Config, cfg.Multiplexer.Tmux.Config, meta.Explicit["multiplexer.tmux.config"], "tmux config path, or auto to resolve project and user configs"},
		{"multiplexer.zellij.bin", def.Multiplexer.Zellij.Bin, cfg.Multiplexer.Zellij.Bin, meta.Explicit["multiplexer.zellij.bin"], "Zellij executable override"},
		{"multiplexer.zellij.config", def.Multiplexer.Zellij.Config, cfg.Multiplexer.Zellij.Config, meta.Explicit["multiplexer.zellij.config"], "Zellij config path override"},
		{"multiplexer.screen.bin", def.Multiplexer.Screen.Bin, cfg.Multiplexer.Screen.Bin, meta.Explicit["multiplexer.screen.bin"], "GNU Screen executable override"},
		{"multiplexer.screen.config", def.Multiplexer.Screen.Config, cfg.Multiplexer.Screen.Config, meta.Explicit["multiplexer.screen.config"], "GNU Screen config path override"},
	}
}
