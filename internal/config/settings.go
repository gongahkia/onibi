package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return FormatDuration(time.Duration(d)) }

func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be scalar, got %s", n.ShortTag())
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
}

type Daemon struct {
	ApprovalTimeout       Duration `yaml:"approval_timeout" json:"approval_timeout"`
	ApprovalSweepInterval Duration `yaml:"approval_sweep_interval" json:"approval_sweep_interval"`
	TurnIdleThreshold     Duration `yaml:"turn_idle_threshold" json:"turn_idle_threshold"`
	TurnIdleInterval      Duration `yaml:"turn_idle_interval" json:"turn_idle_interval"`
	PTYBufferBytes        int      `yaml:"pty_buffer_bytes" json:"pty_buffer_bytes"`
}

type Shell struct {
	MinDuration Duration `yaml:"min_duration" json:"min_duration"`
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
		Daemon: Daemon{
			ApprovalTimeout:       Duration(5 * time.Minute),
			ApprovalSweepInterval: Duration(15 * time.Second),
			TurnIdleThreshold:     Duration(3 * time.Second),
			TurnIdleInterval:      Duration(500 * time.Millisecond),
			PTYBufferBytes:        64 * 1024,
		},
		Shell: Shell{
			MinDuration: Duration(5 * time.Second),
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
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); errors.Is(err, io.EOF) {
		return cfg, meta, nil
	} else if err != nil {
		return cfg, meta, fmt.Errorf("parse %s: %w", path, err)
	}
	applyRaw(&cfg, &meta, raw)
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
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) Validate() error {
	checks := []struct {
		name string
		got  time.Duration
		min  time.Duration
		max  time.Duration
	}{
		{"daemon.approval_timeout", c.Daemon.ApprovalTimeout.Std(), 10 * time.Second, 24 * time.Hour},
		{"daemon.approval_sweep_interval", c.Daemon.ApprovalSweepInterval.Std(), time.Second, 5 * time.Minute},
		{"daemon.turn_idle_threshold", c.Daemon.TurnIdleThreshold.Std(), time.Second, 30 * time.Minute},
		{"daemon.turn_idle_interval", c.Daemon.TurnIdleInterval.Std(), 100 * time.Millisecond, 10 * time.Second},
		{"shell.min_duration", c.Shell.MinDuration.Std(), 0, 24 * time.Hour},
	}
	for _, x := range checks {
		if x.got < x.min || x.got > x.max {
			return fmt.Errorf("%s must be between %s and %s", x.name, x.min, x.max)
		}
	}
	if c.Daemon.PTYBufferBytes < 4096 || c.Daemon.PTYBufferBytes > 10*1024*1024 {
		return fmt.Errorf("daemon.pty_buffer_bytes must be between 4096 and 10485760")
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
	if err != nil {
		return 0, fmt.Errorf("duration %q must be Go duration syntax like 5m or integer seconds", s)
	}
	if n < 0 {
		return 0, errors.New("duration must be non-negative")
	}
	return time.Duration(n) * time.Second, nil
}

func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", d/time.Second)
	}
	if d%time.Millisecond == 0 {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	return d.String()
}

func Set(cfg *Config, key, value string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
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
	case "daemon.turn_idle_threshold":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.TurnIdleThreshold = Duration(d)
	case "daemon.turn_idle_interval":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.TurnIdleInterval = Duration(d)
	case "daemon.pty_buffer_bytes":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("daemon.pty_buffer_bytes must be integer bytes")
		}
		cfg.Daemon.PTYBufferBytes = n
	case "shell.min_duration":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Shell.MinDuration = Duration(d)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return cfg.Validate()
}

func Get(cfg Config, key string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "daemon.approval_timeout":
		return cfg.Daemon.ApprovalTimeout.String(), nil
	case "daemon.approval_sweep_interval":
		return cfg.Daemon.ApprovalSweepInterval.String(), nil
	case "daemon.turn_idle_threshold":
		return cfg.Daemon.TurnIdleThreshold.String(), nil
	case "daemon.turn_idle_interval":
		return cfg.Daemon.TurnIdleInterval.String(), nil
	case "daemon.pty_buffer_bytes":
		return strconv.Itoa(cfg.Daemon.PTYBufferBytes), nil
	case "shell.min_duration":
		return cfg.Shell.MinDuration.String(), nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func Keys(cfg Config, meta LoadMeta) []KeyInfo {
	def := Default()
	rows := []KeyInfo{
		{"daemon.approval_timeout", def.Daemon.ApprovalTimeout.String(), cfg.Daemon.ApprovalTimeout.String(), meta.Explicit["daemon.approval_timeout"], "approval request lifetime before default-deny"},
		{"daemon.approval_sweep_interval", def.Daemon.ApprovalSweepInterval.String(), cfg.Daemon.ApprovalSweepInterval.String(), meta.Explicit["daemon.approval_sweep_interval"], "how often pending approvals are expired"},
		{"daemon.turn_idle_threshold", def.Daemon.TurnIdleThreshold.String(), cfg.Daemon.TurnIdleThreshold.String(), meta.Explicit["daemon.turn_idle_threshold"], "fallback silence window before turn-complete"},
		{"daemon.turn_idle_interval", def.Daemon.TurnIdleInterval.String(), cfg.Daemon.TurnIdleInterval.String(), meta.Explicit["daemon.turn_idle_interval"], "fallback idle poll cadence"},
		{"daemon.pty_buffer_bytes", strconv.Itoa(def.Daemon.PTYBufferBytes), strconv.Itoa(cfg.Daemon.PTYBufferBytes), meta.Explicit["daemon.pty_buffer_bytes"], "bytes retained for /peek text rendering"},
		{"shell.min_duration", def.Shell.MinDuration.String(), cfg.Shell.MinDuration.String(), meta.Explicit["shell.min_duration"], "shell command duration before hooks notify"},
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

type rawConfig struct {
	Daemon rawDaemon `yaml:"daemon"`
	Shell  rawShell  `yaml:"shell"`
}

type rawDaemon struct {
	ApprovalTimeout       *Duration `yaml:"approval_timeout"`
	ApprovalSweepInterval *Duration `yaml:"approval_sweep_interval"`
	TurnIdleThreshold     *Duration `yaml:"turn_idle_threshold"`
	TurnIdleInterval      *Duration `yaml:"turn_idle_interval"`
	PTYBufferBytes        *int      `yaml:"pty_buffer_bytes"`
}

type rawShell struct {
	MinDuration *Duration `yaml:"min_duration"`
}

func applyRaw(cfg *Config, meta *LoadMeta, raw rawConfig) {
	if raw.Daemon.ApprovalTimeout != nil {
		cfg.Daemon.ApprovalTimeout = *raw.Daemon.ApprovalTimeout
		meta.Explicit["daemon.approval_timeout"] = true
	}
	if raw.Daemon.ApprovalSweepInterval != nil {
		cfg.Daemon.ApprovalSweepInterval = *raw.Daemon.ApprovalSweepInterval
		meta.Explicit["daemon.approval_sweep_interval"] = true
	}
	if raw.Daemon.TurnIdleThreshold != nil {
		cfg.Daemon.TurnIdleThreshold = *raw.Daemon.TurnIdleThreshold
		meta.Explicit["daemon.turn_idle_threshold"] = true
	}
	if raw.Daemon.TurnIdleInterval != nil {
		cfg.Daemon.TurnIdleInterval = *raw.Daemon.TurnIdleInterval
		meta.Explicit["daemon.turn_idle_interval"] = true
	}
	if raw.Daemon.PTYBufferBytes != nil {
		cfg.Daemon.PTYBufferBytes = *raw.Daemon.PTYBufferBytes
		meta.Explicit["daemon.pty_buffer_bytes"] = true
	}
	if raw.Shell.MinDuration != nil {
		cfg.Shell.MinDuration = *raw.Shell.MinDuration
		meta.Explicit["shell.min_duration"] = true
	}
}
