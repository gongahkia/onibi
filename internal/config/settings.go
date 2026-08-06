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
	Daemon Daemon `yaml:"daemon" json:"daemon"`
	Shell  Shell  `yaml:"shell" json:"shell"`
	Screen Screen `yaml:"screen" json:"screen"`
}

type Daemon struct {
	ApprovalTimeout       Duration `yaml:"approval_timeout" json:"approval_timeout"`
	ApprovalSweepInterval Duration `yaml:"approval_sweep_interval" json:"approval_sweep_interval"`
	OutputBufferBytes     int      `yaml:"output_buffer_bytes" json:"output_buffer_bytes"`
	MaxSubscribers        int      `yaml:"max_subscribers" json:"max_subscribers"`
}

type Shell struct {
	Default string `yaml:"default" json:"default"`
	Login   bool   `yaml:"login" json:"login"`
}

type Screen struct {
	Font     string `yaml:"font" json:"font"`
	FontPath string `yaml:"font_path" json:"font_path"`
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
		Daemon: Daemon{ApprovalTimeout: Duration(5 * time.Minute), ApprovalSweepInterval: Duration(15 * time.Second), OutputBufferBytes: 64 * 1024, MaxSubscribers: 32},
		Shell:  Shell{Default: "auto", Login: true},
		Screen: Screen{Font: "jetbrains-mono-nerd"},
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
			ApprovalTimeout       *Duration `yaml:"approval_timeout"`
			ApprovalSweepInterval *Duration `yaml:"approval_sweep_interval"`
			OutputBufferBytes     *int      `yaml:"output_buffer_bytes"`
			MaxSubscribers        *int      `yaml:"max_subscribers"`
		} `yaml:"daemon"`
		Shell struct {
			Default *string `yaml:"default"`
			Login   *bool   `yaml:"login"`
		} `yaml:"shell"`
		Screen struct {
			Font     *string `yaml:"font"`
			FontPath *string `yaml:"font_path"`
		} `yaml:"screen"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&raw); err != nil {
		return cfg, meta, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw.Daemon.ApprovalTimeout != nil {
		cfg.Daemon.ApprovalTimeout = *raw.Daemon.ApprovalTimeout
		meta.Explicit["daemon.approval_timeout"] = true
	}
	if raw.Daemon.ApprovalSweepInterval != nil {
		cfg.Daemon.ApprovalSweepInterval = *raw.Daemon.ApprovalSweepInterval
		meta.Explicit["daemon.approval_sweep_interval"] = true
	}
	if raw.Daemon.OutputBufferBytes != nil {
		cfg.Daemon.OutputBufferBytes = *raw.Daemon.OutputBufferBytes
		meta.Explicit["daemon.output_buffer_bytes"] = true
	}
	if raw.Daemon.MaxSubscribers != nil {
		cfg.Daemon.MaxSubscribers = *raw.Daemon.MaxSubscribers
		meta.Explicit["daemon.max_subscribers"] = true
	}
	if raw.Shell.Default != nil {
		cfg.Shell.Default = strings.TrimSpace(*raw.Shell.Default)
		meta.Explicit["shell.default"] = true
	}
	if raw.Shell.Login != nil {
		cfg.Shell.Login = *raw.Shell.Login
		meta.Explicit["shell.login"] = true
	}
	if raw.Screen.Font != nil {
		cfg.Screen.Font = strings.TrimSpace(*raw.Screen.Font)
		meta.Explicit["screen.font"] = true
	}
	if raw.Screen.FontPath != nil {
		cfg.Screen.FontPath = strings.TrimSpace(*raw.Screen.FontPath)
		meta.Explicit["screen.font_path"] = true
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
	if c.Daemon.ApprovalTimeout.Std() < 10*time.Second || c.Daemon.ApprovalTimeout.Std() > 5*time.Minute {
		return errors.New("daemon.approval_timeout must be between 10s and 5m")
	}
	if c.Daemon.ApprovalSweepInterval.Std() < time.Second || c.Daemon.ApprovalSweepInterval.Std() > 5*time.Minute {
		return errors.New("daemon.approval_sweep_interval must be between 1s and 5m")
	}
	if c.Daemon.OutputBufferBytes < 4096 || c.Daemon.OutputBufferBytes > 10*1024*1024 {
		return errors.New("daemon.output_buffer_bytes must be between 4096 and 10485760")
	}
	if c.Daemon.MaxSubscribers < 1 || c.Daemon.MaxSubscribers > 4096 {
		return errors.New("daemon.max_subscribers must be between 1 and 4096")
	}
	if strings.TrimSpace(c.Shell.Default) == "" {
		return errors.New("shell.default required")
	}
	switch c.Screen.Font {
	case "jetbrains-mono-nerd", "caskaydia-cove-nerd", "go-mono-nerd":
	case "custom":
		if c.Screen.FontPath == "" {
			return errors.New("screen.font_path required when screen.font=custom")
		}
		info, err := os.Stat(c.Screen.FontPath)
		if err != nil || info.IsDir() {
			return errors.New("screen.font_path must be a readable font file")
		}
	default:
		return errors.New("screen.font must be jetbrains-mono-nerd, caskaydia-cove-nerd, go-mono-nerd, or custom")
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
	case "daemon.approval_timeout":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.ApprovalTimeout = Duration(d)
	case "daemon.approval_sweep_interval":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.ApprovalSweepInterval = Duration(d)
	case "daemon.output_buffer_bytes":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Daemon.OutputBufferBytes = n
	case "daemon.max_subscribers":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Daemon.MaxSubscribers = n
	case "shell.default":
		cfg.Shell.Default = strings.TrimSpace(value)
	case "shell.login":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Shell.Login = v
	case "screen.font":
		cfg.Screen.Font = strings.TrimSpace(value)
	case "screen.font_path":
		cfg.Screen.FontPath = strings.TrimSpace(value)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return cfg.Validate()
}

func Get(cfg Config, key string) (string, error) {
	switch strings.TrimSpace(key) {
	case "daemon.approval_timeout":
		return cfg.Daemon.ApprovalTimeout.String(), nil
	case "daemon.approval_sweep_interval":
		return cfg.Daemon.ApprovalSweepInterval.String(), nil
	case "daemon.output_buffer_bytes":
		return strconv.Itoa(cfg.Daemon.OutputBufferBytes), nil
	case "daemon.max_subscribers":
		return strconv.Itoa(cfg.Daemon.MaxSubscribers), nil
	case "shell.default":
		return cfg.Shell.Default, nil
	case "shell.login":
		return strconv.FormatBool(cfg.Shell.Login), nil
	case "screen.font":
		return cfg.Screen.Font, nil
	case "screen.font_path":
		return cfg.Screen.FontPath, nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func Keys(cfg Config, meta LoadMeta) []KeyInfo {
	def := Default()
	return []KeyInfo{
		{"daemon.approval_timeout", def.Daemon.ApprovalTimeout.String(), cfg.Daemon.ApprovalTimeout.String(), meta.Explicit["daemon.approval_timeout"], "approval lifetime before default denial"},
		{"daemon.approval_sweep_interval", def.Daemon.ApprovalSweepInterval.String(), cfg.Daemon.ApprovalSweepInterval.String(), meta.Explicit["daemon.approval_sweep_interval"], "pending-approval expiry cadence"},
		{"daemon.output_buffer_bytes", strconv.Itoa(def.Daemon.OutputBufferBytes), strconv.Itoa(cfg.Daemon.OutputBufferBytes), meta.Explicit["daemon.output_buffer_bytes"], "output retained for Telegram tail and screen capture"},
		{"daemon.max_subscribers", strconv.Itoa(def.Daemon.MaxSubscribers), strconv.Itoa(cfg.Daemon.MaxSubscribers), meta.Explicit["daemon.max_subscribers"], "maximum internal decision subscribers"},
		{"shell.default", def.Shell.Default, cfg.Shell.Default, meta.Explicit["shell.default"], "default shell for new tmux sessions"},
		{"shell.login", strconv.FormatBool(def.Shell.Login), strconv.FormatBool(cfg.Shell.Login), meta.Explicit["shell.login"], "run default shell as a login shell"},
		{"screen.font", def.Screen.Font, cfg.Screen.Font, meta.Explicit["screen.font"], "font used for rendered terminal screens"},
		{"screen.font_path", def.Screen.FontPath, cfg.Screen.FontPath, meta.Explicit["screen.font_path"], "external TTF/OTF path when screen.font=custom"},
	}
}
