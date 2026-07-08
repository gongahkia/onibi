package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/buildinfo"
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
	if err := checkOnibiVersion(m.MinOnibiVersion, buildinfo.Version); err != nil {
		return fmt.Errorf("adapter %q %w", name, err)
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
	commands := append([]string{}, m.HookInstall...)
	commands = append(commands, m.HookUninstall...)
	commands = append(commands, m.HookVerify...)
	commands = append(commands, m.HookAdopt...)
	for _, cmd := range commands {
		if err := validateHookCommand(cmd); err != nil {
			return fmt.Errorf("adapter %q command rejected: %w", name, err)
		}
	}
	return nil
}

func checkOnibiVersion(minVersion, runningVersion string) error {
	min, err := parseSemver(minVersion)
	if err != nil {
		return err
	}
	running, err := parseSemver(runningVersion)
	if err != nil {
		return fmt.Errorf("cannot verify running Onibi version %q", runningVersion)
	}
	if compareSemver(running, min) < 0 {
		return fmt.Errorf("requires Onibi >= %s, running %s", minVersion, runningVersion)
	}
	return nil
}

type semverParts struct {
	major int
	minor int
	patch int
}

func parseSemver(version string) (semverParts, error) {
	original := version
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	i := 0
	for i < len(version) {
		c := version[i]
		if (c >= '0' && c <= '9') || c == '.' {
			i++
			continue
		}
		break
	}
	core := version[:i]
	if core == "" {
		return semverParts{}, fmt.Errorf("invalid semver %q", original)
	}
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return semverParts{}, fmt.Errorf("invalid semver %q", original)
	}
	nums := [3]int{}
	for i, part := range parts {
		if part == "" {
			return semverParts{}, fmt.Errorf("invalid semver %q", original)
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return semverParts{}, fmt.Errorf("invalid semver %q", original)
		}
		nums[i] = n
	}
	return semverParts{major: nums[0], minor: nums[1], patch: nums[2]}, nil
}

func compareSemver(a, b semverParts) int {
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	return a.patch - b.patch
}

func WithRuntimeAdapter(m Manifest) Manifest {
	if m.Adapter.Name != "" && !m.runtimeAdapter {
		return m
	}
	m.Adapter = manifestAdapter(m)
	m.runtimeAdapter = true
	return m
}

func manifestAdapter(m Manifest) Adapter {
	version := m.Version
	return Adapter{
		Name: m.Name,
		Install: func(ctx context.Context, db *store.DB, notifyBin string) error {
			if err := runManifestCommands(ctx, m, notifyBin, m.HookInstall); err != nil {
				return err
			}
			return recordManifestProvenance(ctx, db, m)
		},
		Uninstall: func(ctx context.Context, db *store.DB) error {
			if err := runManifestCommands(ctx, m, "", m.HookUninstall); err != nil {
				return err
			}
			return deleteManifestProvenance(ctx, db, m)
		},
		Status: func(ctx context.Context, db *store.DB) common.Info {
			info := common.Info{
				Name:             m.Name,
				Support:          string(m.Kind),
				Installed:        true,
				Managed:          true,
				InstalledVersion: common.VersionPtr(version),
				BundledVersion:   version,
				InstallPath:      m.SourcePath,
				Message:          "third-party adapter manifest loaded",
			}
			body, err := manifestProvenanceBody(m)
			if err != nil {
				info.Installed = false
				info.Managed = false
				info.InstallPath = ""
				info.Message = "third-party adapter manifest source missing"
				return info
			}
			if len(body) > 0 && db != nil {
				common.ApplyManagedStatus(ctx, db, &info, m.Name, m.SourcePath, body, "third-party adapter manifest loaded", "onibi install-hooks --agent "+m.Name)
			}
			return info
		},
		Verify: func(ctx context.Context, db *store.DB) error {
			if err := runManifestCommands(ctx, m, "", m.HookVerify); err != nil {
				return err
			}
			body, err := manifestProvenanceBody(m)
			if err != nil {
				return err
			}
			if len(body) == 0 || db == nil {
				return nil
			}
			return common.VerifyRecorded(ctx, db, m.Name, m.SourcePath, body)
		},
		Adopt: func(ctx context.Context, db *store.DB) error {
			if err := runManifestCommands(ctx, m, "", m.HookAdopt); err != nil {
				return err
			}
			return recordManifestProvenance(ctx, db, m)
		},
	}
}

func manifestProvenanceBody(m Manifest) ([]byte, error) {
	if strings.TrimSpace(m.SourcePath) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(m.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("read adapter manifest provenance %s: %w", m.SourcePath, err)
	}
	return body, nil
}

func recordManifestProvenance(ctx context.Context, db *store.DB, m Manifest) error {
	if db == nil {
		return nil
	}
	body, err := manifestProvenanceBody(m)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return common.Record(ctx, db, m.Name, m.SourcePath, body)
}

func deleteManifestProvenance(ctx context.Context, db *store.DB, m Manifest) error {
	if db == nil || strings.TrimSpace(m.SourcePath) == "" {
		return nil
	}
	return common.DeleteRecord(ctx, db, m.Name, m.SourcePath)
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
