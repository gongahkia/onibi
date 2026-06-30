package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

var (
	manifestNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	semverRe       = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

func ParseManifest(data []byte, sourcePath string) (Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	m.SourcePath = sourcePath
	if err := ValidateManifest(m); err != nil {
		return Manifest{}, err
	}
	return WithRuntimeAdapter(m), nil
}

func ValidateManifest(m Manifest) error {
	name := normalizeName(m.Name)
	if !manifestNameRe.MatchString(name) {
		return fmt.Errorf("invalid adapter name %q", m.Name)
	}
	if !semverRe.MatchString(m.Version) {
		return fmt.Errorf("adapter %q invalid version %q", name, m.Version)
	}
	switch m.Kind {
	case KindAgent, KindShell:
	default:
		return fmt.Errorf("adapter %q invalid kind %q", name, m.Kind)
	}
	if len(m.CmdPattern) == 0 {
		return fmt.Errorf("adapter %q cmd_pattern required", name)
	}
	for event, pattern := range m.CmdPattern {
		if strings.TrimSpace(event) == "" || strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("adapter %q invalid cmd_pattern entry", name)
		}
	}
	if len(m.HookInstall) == 0 {
		return fmt.Errorf("adapter %q hook_install required", name)
	}
	if len(m.HookUninstall) == 0 {
		return fmt.Errorf("adapter %q hook_uninstall required", name)
	}
	if !semverRe.MatchString(m.MinOnibiVersion) {
		return fmt.Errorf("adapter %q invalid min_onibi_version %q", name, m.MinOnibiVersion)
	}
	if m.RiskOverrides == nil {
		return fmt.Errorf("adapter %q risk_overrides required", name)
	}
	for pattern, level := range m.RiskOverrides {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("adapter %q empty risk override pattern", name)
		}
		switch level {
		case "low", "medium", "high", "critical":
		default:
			return fmt.Errorf("adapter %q invalid risk level %q", name, level)
		}
	}
	for _, cmd := range append(append([]string{}, m.HookInstall...), m.HookUninstall...) {
		if err := validateHookCommand(cmd); err != nil {
			return fmt.Errorf("adapter %q command rejected: %w", name, err)
		}
	}
	return nil
}

func WithRuntimeAdapter(m Manifest) Manifest {
	if m.Adapter.Name != "" {
		return m
	}
	m.Adapter = manifestAdapter(m)
	return m
}

func manifestAdapter(m Manifest) Adapter {
	version := m.Version
	return Adapter{
		Name: m.Name,
		Install: func(ctx context.Context, _ *store.DB, notifyBin string) error {
			return runManifestCommands(ctx, m, notifyBin, m.HookInstall)
		},
		Uninstall: func(ctx context.Context, _ *store.DB) error { return runManifestCommands(ctx, m, "", m.HookUninstall) },
		Status: func(context.Context, *store.DB) common.Info {
			return common.Info{
				Name:             m.Name,
				Support:          string(m.Kind),
				Installed:        true,
				Managed:          true,
				InstalledVersion: common.VersionPtr(version),
				BundledVersion:   version,
				InstallPath:      m.SourcePath,
				Message:          "third-party adapter manifest loaded",
			}
		},
		Verify: func(context.Context, *store.DB) error { return nil },
		Adopt:  func(context.Context, *store.DB) error { return nil },
	}
}

func validateHookCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return errors.New("empty command")
	}
	for _, token := range []string{"`", "$(", "<(", ">("} {
		if strings.Contains(cmd, token) {
			return fmt.Errorf("restricted shell token %q", token)
		}
	}
	fields := strings.Fields(cmd)
	for _, field := range fields {
		switch strings.Trim(field, `"'`) {
		case "curl", "wget", "nc", "ncat", "socat", "sudo", "su", "doas":
			return fmt.Errorf("restricted command %q", field)
		}
	}
	lower := strings.ToLower(cmd)
	for _, bad := range []string{"rm -rf /", "rm -rf ~", "rm -rf $home", "rm -rf ${home}"} {
		if strings.Contains(lower, bad) {
			return fmt.Errorf("restricted destructive command %q", bad)
		}
	}
	if strings.Contains(lower, ">/dev/") || strings.Contains(lower, ">>/dev/") {
		return errors.New("device redirection is restricted")
	}
	return nil
}

func runManifestCommands(ctx context.Context, m Manifest, notifyBin string, commands []string) error {
	for _, raw := range commands {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", raw)
		cmd.Env = append(os.Environ(),
			"ONIBI_ADAPTER_NAME="+m.Name,
			"ONIBI_ADAPTER_VERSION="+m.Version,
			"ONIBI_NOTIFY_BIN="+notifyBin,
		)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w: %s", raw, err, strings.TrimSpace(out.String()))
		}
	}
	return nil
}
