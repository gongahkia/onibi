package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/pelletier/go-toml/v2"
)

const ProjectRelPath = ".onibi/workspace.toml"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type ProjectConfig struct {
	SchemaVersion int    `toml:"schema_version"`
	Name          string `toml:"name"`
	DefaultAgent  string `toml:"default_agent,omitempty"`
}

func ProjectPath(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("workspace root required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", root)
	}
	return filepath.Join(root, ProjectRelPath), nil
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
	if err := ValidateProjectConfig(cfg); err != nil {
		return ProjectConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func SaveProjectConfig(path string, cfg ProjectConfig) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	if err := ValidateProjectConfig(cfg); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".workspace-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ValidateProjectConfig(cfg ProjectConfig) error {
	if cfg.SchemaVersion != 1 {
		return errors.New("schema_version must be 1")
	}
	if !namePattern.MatchString(cfg.Name) {
		return fmt.Errorf("name %q must match %s", cfg.Name, namePattern.String())
	}
	if cfg.DefaultAgent == "" {
		return nil
	}
	if strings.TrimSpace(cfg.DefaultAgent) != cfg.DefaultAgent {
		return fmt.Errorf("default_agent %q has surrounding whitespace", cfg.DefaultAgent)
	}
	if _, err := adapters.ManifestFor(cfg.DefaultAgent); err != nil {
		return fmt.Errorf("default_agent: %w", err)
	}
	return nil
}
