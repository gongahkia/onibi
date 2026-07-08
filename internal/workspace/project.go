package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const ProjectRelPath = ".onibi/workspace.toml"

type ProjectFile struct {
	Root string
	Path string
}

type ProjectConfig struct {
	SchemaVersion int               `toml:"schema_version"`
	Name          string            `toml:"name"`
	DefaultAgent  string            `toml:"default_agent,omitempty"`
	Trust         ProjectTrust      `toml:"trust,omitempty"`
	Budget        ProjectBudget     `toml:"budget,omitempty"`
	Transports    ProjectTransports `toml:"transports,omitempty"`
	Hooks         ProjectHooks      `toml:"hooks,omitempty"`
}

type ProjectTrust struct {
	PolicyFile string             `toml:"policy_file,omitempty"`
	Rule       []ProjectTrustRule `toml:"rule,omitempty"`
}

type ProjectTrustRule struct {
	Effect  string            `toml:"effect"`
	Expires string            `toml:"expires,omitempty"`
	Match   map[string]string `toml:"match,omitempty"`
}

type ProjectBudget struct {
	Global  ProjectGlobalBudget  `toml:"global,omitempty"`
	Session ProjectSessionBudget `toml:"session,omitempty"`
}

type ProjectGlobalBudget struct {
	MaxTokensPerDay int `toml:"max_tokens_per_day,omitempty"`
}

type ProjectSessionBudget struct {
	MaxTokens int    `toml:"max_tokens,omitempty"`
	OnOverrun string `toml:"on_overrun,omitempty"`
}

type ProjectTransports struct {
	Default string   `toml:"default,omitempty"`
	Web     []string `toml:"web,omitempty"`
	Chat    []string `toml:"chat,omitempty"`
	Notify  []string `toml:"notify,omitempty"`
}

type ProjectHooks struct {
	AutoInstall []string `toml:"auto_install,omitempty"`
}

func FindProjectFile(start string) (ProjectFile, bool, error) {
	if strings.TrimSpace(start) == "" {
		return ProjectFile{}, false, errors.New("workspace search start required")
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return ProjectFile{}, false, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return ProjectFile{}, false, err
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		path := filepath.Join(dir, ProjectRelPath)
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return ProjectFile{}, false, fmt.Errorf("%s is a directory", path)
			}
			return ProjectFile{Root: dir, Path: path}, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return ProjectFile{}, false, err
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ProjectFile{}, false, nil
		}
		dir = next
	}
}

func LoadProjectConfig(path string) (ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectConfig{}, err
	}
	var cfg ProjectConfig
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&cfg); err != nil {
		return ProjectConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.SchemaVersion != 1 {
		return ProjectConfig{}, fmt.Errorf("%s: schema_version must be 1", path)
	}
	if err := validateName(cfg.Name); err != nil {
		return ProjectConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Transports.Default = strings.ToLower(strings.TrimSpace(cfg.Transports.Default))
	if err := validateProjectTransports(path, &cfg.Transports); err != nil {
		return ProjectConfig{}, err
	}
	return cfg, nil
}

func SaveProjectConfig(path string, cfg ProjectConfig) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	cfg.Transports.Default = strings.ToLower(strings.TrimSpace(cfg.Transports.Default))
	if cfg.SchemaVersion != 1 {
		return fmt.Errorf("%s: schema_version must be 1", path)
	}
	if err := validateName(cfg.Name); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := validateProjectTransports(path, &cfg.Transports); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func validateProjectTransports(path string, transports *ProjectTransports) error {
	if transports == nil {
		return nil
	}
	if transports.Default != "" {
		if err := validateTransportMode(transports.Default); err != nil {
			return fmt.Errorf("%s: transports.default: %w", path, err)
		}
	}
	for key, values := range map[string][]string{
		"transports.web":    transports.Web,
		"transports.chat":   transports.Chat,
		"transports.notify": transports.Notify,
	} {
		for i, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if err := validateTransportMode(value); err != nil {
				return fmt.Errorf("%s: %s[%d]: %w", path, key, i, err)
			}
		}
	}
	return nil
}

func validateTransportMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "lan", "tailscale", "wireguard", "zerotier", "cloudflare-quick", "cloudflare-named", "ngrok", "telegram", "matrix", "slack", "discord", "pushover", "ntfy", "gotify", "auto":
		return nil
	default:
		return fmt.Errorf("unknown transport %q", mode)
	}
}
