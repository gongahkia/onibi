package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/trust"
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
	Match   ProjectTrustMatch `toml:"match,omitempty"`
}

type ProjectTrustMatch struct {
	Tool  string `toml:"tool,omitempty"`
	Path  string `toml:"path,omitempty"`
	Agent string `toml:"agent,omitempty"`
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
	if err := validateProjectPaths(path, &cfg); err != nil {
		return ProjectConfig{}, err
	}
	if err := validateProjectPolicies(path, &cfg); err != nil {
		return ProjectConfig{}, err
	}
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
	if err := validateProjectPaths(path, &cfg); err != nil {
		return err
	}
	if err := validateProjectPolicies(path, &cfg); err != nil {
		return err
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

func validateProjectPaths(path string, cfg *ProjectConfig) error {
	if cfg == nil {
		return nil
	}
	return validateProjectRelativePath(path, "trust.policy_file", cfg.Trust.PolicyFile)
}

func validateProjectPolicies(path string, cfg *ProjectConfig) error {
	if cfg == nil {
		return nil
	}
	trustPolicy := trust.Policy{Rules: make([]trust.Rule, len(cfg.Trust.Rule))}
	for i, rule := range cfg.Trust.Rule {
		trustPolicy.Rules[i] = trust.Rule{
			Effect:     trust.Effect(rule.Effect),
			ExpiresRaw: rule.Expires,
			Match: trust.Match{
				Tool:  rule.Match.Tool,
				Path:  rule.Match.Path,
				Agent: rule.Match.Agent,
			},
		}
	}
	if err := trustPolicy.Validate(); err != nil {
		return fmt.Errorf("%s: trust.rule: %w", path, err)
	}
	budgetPolicy := budget.DefaultPolicy()
	budgetPolicy.Global.MaxTokensPerDay = int64(cfg.Budget.Global.MaxTokensPerDay)
	budgetPolicy.Session.MaxTokens = int64(cfg.Budget.Session.MaxTokens)
	if cfg.Budget.Session.OnOverrun != "" {
		budgetPolicy.Session.OnOverrun = budget.OverrunAction(cfg.Budget.Session.OnOverrun)
	}
	if err := budgetPolicy.Validate(); err != nil {
		return fmt.Errorf("%s: budget: %w", path, err)
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

func validateProjectRelativePath(configPath, key, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if filepath.IsAbs(value) {
		return fmt.Errorf("%s: %s must be a relative path", configPath, key)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("%s: resolve workspace root: %w", configPath, err)
	}
	workspaceDir := filepath.Dir(absConfigPath)
	workspaceRoot := workspaceDir
	if filepath.Base(workspaceDir) == ".onibi" {
		workspaceRoot = filepath.Dir(workspaceDir)
	}
	resolved := filepath.Clean(filepath.Join(workspaceDir, value))
	rel, err := filepath.Rel(workspaceRoot, resolved)
	if err != nil {
		return fmt.Errorf("%s: %s resolve: %w", configPath, key, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s: %s must stay inside workspace root", configPath, key)
	}
	return nil
}

func validateTransportMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "lan", "tailscale", "tailscale-private", "wireguard", "zerotier", "cloudflare-quick", "cloudflare-named", "ngrok", "telegram", "matrix", "slack", "discord", "zulip", "irc", "signal", "pushover", "ntfy", "gotify", "apns", "sms", "email", "auto":
		return nil
	default:
		return fmt.Errorf("unknown transport %q", mode)
	}
}
