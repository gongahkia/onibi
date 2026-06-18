// Package adapters discovers and installs coding-agent integrations.
package adapters

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/amp"
	"github.com/gongahkia/onibi/internal/adapters/claude"
	"github.com/gongahkia/onibi/internal/adapters/codex"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/adapters/copilot"
	"github.com/gongahkia/onibi/internal/adapters/gemini"
	"github.com/gongahkia/onibi/internal/adapters/goose"
	"github.com/gongahkia/onibi/internal/adapters/opencode"
	"github.com/gongahkia/onibi/internal/adapters/pi"
	"github.com/gongahkia/onibi/internal/adapters/shell"
	"github.com/gongahkia/onibi/internal/store"
)

type Adapter struct {
	Name              string
	Install           func(context.Context, *store.DB, string) error
	Uninstall         func(context.Context, *store.DB) error
	Status            func(context.Context, *store.DB) common.Info
	Verify            func(context.Context, *store.DB) error
	Adopt             func(context.Context, *store.DB) error
	ExpectedHooks     func(string) ([]common.ExpectedHook, error)
	ObservedHooks     func() ([]common.ObservedHook, error)
	TrustInstructions func() []string
	BackupPath        func(context.Context, *store.DB) string
}

func Registry() map[string]Adapter {
	return map[string]Adapter{
		"amp":      {Name: "amp", Install: amp.Install, Uninstall: amp.Uninstall, Status: amp.Status, Verify: amp.VerifyHash, Adopt: amp.Adopt},
		"claude":   {Name: "claude", Install: claude.Install, Uninstall: claude.Uninstall, Status: claudeStatus, Verify: claude.VerifyHash, Adopt: claude.Adopt},
		"codex":    {Name: "codex", Install: codex.Install, Uninstall: codex.Uninstall, Status: codex.Status, Verify: codex.VerifyHash, Adopt: codex.Adopt, ExpectedHooks: codex.ExpectedHooks, ObservedHooks: codex.ObservedHooks, TrustInstructions: codex.TrustInstructions, BackupPath: codex.BackupPath},
		"copilot":  {Name: "copilot", Install: copilot.Install, Uninstall: copilot.Uninstall, Status: copilot.Status, Verify: copilot.VerifyHash, Adopt: copilot.Adopt},
		"gemini":   {Name: "gemini", Install: gemini.Install, Uninstall: gemini.Uninstall, Status: gemini.Status, Verify: gemini.VerifyHash, Adopt: gemini.Adopt},
		"goose":    {Name: "goose", Install: goose.Install, Uninstall: goose.Uninstall, Status: goose.Status, Verify: goose.VerifyHash, Adopt: goose.Adopt},
		"opencode": {Name: "opencode", Install: opencode.Install, Uninstall: opencode.Uninstall, Status: opencode.Status, Verify: opencode.VerifyHash, Adopt: opencode.Adopt},
		"pi":       {Name: "pi", Install: pi.Install, Uninstall: pi.Uninstall, Status: pi.Status, Verify: pi.VerifyHash, Adopt: pi.Adopt},
	}
}

func claudeStatus(ctx context.Context, db *store.DB) common.Info {
	path, err := claude.SettingsPath()
	if err != nil {
		return common.Info{Name: "claude", Support: "blocking", BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	info := common.Info{Name: "claude", Support: "blocking", BundledVersion: common.IntegrationVersion, InstallPath: path}
	body, err := claude.ManagedBody(path)
	if err != nil {
		if strings.Contains(err.Error(), "onibi-managed Stop or PreToolUse hook is missing") {
			common.MarkNotInstalled(&info)
			return info
		}
		info.Message = err.Error()
		return info
	}
	version := claude.InstalledVersion(path)
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, "claude", path, body, "Claude hooks installed", "onibi install-hooks --agent claude")
	return info
}

func Names() []string {
	names := make([]string, 0, len(Registry()))
	for name := range Registry() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Get(name string) (Adapter, bool) {
	a, ok := Registry()[strings.ToLower(strings.TrimSpace(name))]
	return a, ok
}

func InstallShell(ctx context.Context, db *store.DB, notifyBin, name string, minMS int64) error {
	return shell.Install(ctx, db, notifyBin, strings.ToLower(strings.TrimSpace(name)), minMS)
}

func UninstallShell(ctx context.Context, db *store.DB, name string) error {
	return shell.Uninstall(ctx, db, strings.ToLower(strings.TrimSpace(name)))
}

func ShellStatus(ctx context.Context, db *store.DB, name string) common.Info {
	return shell.Status(ctx, db, strings.ToLower(strings.TrimSpace(name)))
}

func VerifyShell(ctx context.Context, db *store.DB, name string) error {
	return shell.VerifyHash(ctx, db, strings.ToLower(strings.TrimSpace(name)))
}

func AdoptShell(ctx context.Context, db *store.DB, name string) error {
	return shell.Adopt(ctx, db, strings.ToLower(strings.TrimSpace(name)))
}

func ShellNames() []string { return shell.Supported() }

func Unsupported(name string) error {
	return fmt.Errorf("adapter %q not supported; available: %s", name, strings.Join(Names(), ", "))
}
