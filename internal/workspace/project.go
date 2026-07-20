package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters"
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
	Transports    ProjectTransports `toml:"transports,omitempty"`
	Hooks         ProjectHooks      `toml:"hooks,omitempty"`
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
	data, err = stripLegacyPolicyTables(data)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("parse %s: %w", path, err)
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
	if err := validateProjectAgents(path, &cfg); err != nil {
		return ProjectConfig{}, err
	}
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
	if err := validateProjectAgents(path, &cfg); err != nil {
		return err
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

func stripLegacyPolicyTables(data []byte) ([]byte, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	delete(raw, "budget")
	delete(raw, "trust")
	return toml.Marshal(raw)
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

func validateProjectAgents(path string, cfg *ProjectConfig) error {
	if cfg == nil {
		return nil
	}
	if err := validateProjectAgent(cfg.DefaultAgent); err != nil {
		return fmt.Errorf("%s: default_agent: %w", path, err)
	}
	for i, target := range cfg.Hooks.AutoInstall {
		if err := validateProjectHook(target); err != nil {
			return fmt.Errorf("%s: hooks.auto_install[%d]: %w", path, i, err)
		}
	}
	return nil
}

func validateProjectAgent(name string) error {
	if name == "" {
		return nil
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("invalid agent %q", name)
	}
	if _, err := adapters.ManifestFor(name); err != nil {
		return err
	}
	return nil
}

func validateProjectHook(target string) error {
	if strings.TrimSpace(target) != target || target == "" {
		return fmt.Errorf("invalid hook %q", target)
	}
	if shell, ok := strings.CutPrefix(target, "shell:"); ok {
		for _, candidate := range adapters.ShellNames() {
			if shell == candidate {
				return nil
			}
		}
		return fmt.Errorf("unsupported shell %q", shell)
	}
	return validateProjectAgent(target)
}

func validateTransportMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "lan", "tailscale", "tailscale-private", "wireguard", "zerotier", "cloudflare-quick", "cloudflare-named", "ngrok", "telegram", "matrix", "slack", "auto":
		return nil
	case "email", "sms", "apns", "gotify", "ntfy", "pushover", "signal", "irc", "zulip", "discord":
		return fmt.Errorf("transport %q is no longer supported; use web push or telegram", mode)
	default:
		return fmt.Errorf("unknown transport %q", mode)
	}
}
