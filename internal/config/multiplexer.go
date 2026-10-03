package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const LocalConfigName = ".onibi.yaml"

// ResolvedConfig describes the global configuration and the optional
// project-local multiplexer override selected for a session working directory.
type ResolvedConfig struct {
	Config Config
	Global LoadMeta
	Local  LoadMeta
	CWD    string
}

// ResolveForCWD layers a project's .onibi.yaml multiplexer section over the
// normal user configuration. Local files deliberately affect only terminal
// backend selection: a checked-out project must not be able to alter daemon
// timeouts, credential storage, or other global daemon behaviour.
func ResolveForCWD(paths Paths, cwd string) (ResolvedConfig, error) {
	cfg, global, err := Load(paths)
	if err != nil {
		return ResolvedConfig{}, err
	}
	if strings.TrimSpace(cwd) == "" {
		return ResolvedConfig{Config: cfg, Global: global}, nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("resolve working directory: %w", err)
	}
	local := LoadMeta{Path: filepath.Join(abs, LocalConfigName), Explicit: map[string]bool{}}
	b, err := os.ReadFile(local.Path)
	if errors.Is(err, os.ErrNotExist) {
		return ResolvedConfig{Config: cfg, Global: global, Local: local, CWD: abs}, nil
	}
	if err != nil {
		return ResolvedConfig{}, err
	}
	local.Exists = true
	if err := applyLocalMultiplexer(&cfg, b, abs, &local); err != nil {
		return ResolvedConfig{}, fmt.Errorf("parse %s: %w", local.Path, err)
	}
	if err := cfg.Validate(); err != nil {
		return ResolvedConfig{}, fmt.Errorf("validate %s: %w", local.Path, err)
	}
	return ResolvedConfig{Config: cfg, Global: global, Local: local, CWD: abs}, nil
}

func applyLocalMultiplexer(cfg *Config, b []byte, cwd string, meta *LoadMeta) error {
	var raw struct {
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
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw.Multiplexer.Default != nil {
		cfg.Multiplexer.Default = strings.ToLower(strings.TrimSpace(*raw.Multiplexer.Default))
		meta.Explicit["multiplexer.default"] = true
	}
	applyLocalBackend(&cfg.Multiplexer.Tmux, raw.Multiplexer.Tmux.Bin, raw.Multiplexer.Tmux.Config, "tmux", cwd, meta)
	applyLocalBackend(&cfg.Multiplexer.Zellij, raw.Multiplexer.Zellij.Bin, raw.Multiplexer.Zellij.Config, "zellij", cwd, meta)
	applyLocalBackend(&cfg.Multiplexer.Screen, raw.Multiplexer.Screen.Bin, raw.Multiplexer.Screen.Config, "screen", cwd, meta)
	return nil
}

func applyLocalBackend(dst *MuxBackend, bin, configPath *string, name, cwd string, meta *LoadMeta) {
	if bin != nil {
		dst.Bin = strings.TrimSpace(*bin)
		meta.Explicit["multiplexer."+name+".bin"] = true
	}
	if configPath != nil {
		dst.Config = resolveProjectPath(cwd, *configPath)
		meta.Explicit["multiplexer."+name+".config"] = true
	}
}

func resolveProjectPath(cwd, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "auto" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(cwd, value)
}

func (m Multiplexer) Backend(name string) (MuxBackend, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "tmux":
		return m.Tmux, nil
	case "zellij":
		return m.Zellij, nil
	case "screen":
		return m.Screen, nil
	default:
		return MuxBackend{}, fmt.Errorf("unsupported multiplexer %q", name)
	}
}

// ResolveConfigPath checks an explicit backend configuration file and makes
// project-relative values absolute. "auto" leaves configuration discovery to
// the multiplexer except for tmux and screen, where ResolveTmuxConfig and
// ResolveScreenConfig provide deterministic project/user discovery.
func ResolveConfigPath(cwd, value string) (string, error) {
	value = resolveProjectPath(cwd, value)
	if value == "" || value == "auto" {
		return "", nil
	}
	info, err := os.Stat(value)
	if err != nil {
		return "", fmt.Errorf("config %s: %w", value, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("config %s is a directory", value)
	}
	return value, nil
}

// ResolveTmuxConfig prefers a project .tmux.conf, then standard user config
// locations. An explicit multiplexer.tmux.config always wins.
func ResolveTmuxConfig(cwd, configured string) (string, error) {
	if path, err := ResolveConfigPath(cwd, configured); err != nil || path != "" || strings.TrimSpace(configured) != "auto" {
		return path, err
	}
	candidates := []string{filepath.Join(cwd, ".tmux.conf")}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".config", "tmux", "tmux.conf"), filepath.Join(home, ".tmux.conf"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", nil
}

// ResolveScreenConfig mirrors tmux's project-first behaviour for GNU Screen.
func ResolveScreenConfig(cwd, configured string) (string, error) {
	if path, err := ResolveConfigPath(cwd, configured); err != nil || path != "" || strings.TrimSpace(configured) != "auto" {
		return path, err
	}
	candidates := []string{filepath.Join(cwd, ".screenrc")}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".screenrc"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", nil
}
