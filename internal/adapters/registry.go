// Package adapters discovers and installs coding-agent integrations.
package adapters

import (
	"context"
	"fmt"
	"sort"
	"strings"

	_ "github.com/gongahkia/onibi/internal/adapters/amp"
	"github.com/gongahkia/onibi/internal/adapters/catalog"
	_ "github.com/gongahkia/onibi/internal/adapters/claude"
	_ "github.com/gongahkia/onibi/internal/adapters/codex"
	"github.com/gongahkia/onibi/internal/adapters/common"
	_ "github.com/gongahkia/onibi/internal/adapters/copilot"
	_ "github.com/gongahkia/onibi/internal/adapters/gemini"
	_ "github.com/gongahkia/onibi/internal/adapters/goose"
	_ "github.com/gongahkia/onibi/internal/adapters/opencode"
	_ "github.com/gongahkia/onibi/internal/adapters/pi"
	"github.com/gongahkia/onibi/internal/adapters/shell"
	"github.com/gongahkia/onibi/internal/store"
)

type Adapter = catalog.Adapter
type Manifest = catalog.Manifest
type Registry interface {
	Register(Manifest) error
	List() []Manifest
	Get(string) (Manifest, error)
}

func NewRegistry() Registry { return catalog.NewRegistry() }

func Register(manifest Manifest) error { return catalog.Register(catalog.WithRuntimeAdapter(manifest)) }

func List() []Manifest {
	_ = LoadExternalManifests()
	return catalog.List()
}

func ManifestFor(name string) (Manifest, error) {
	if err := LoadExternalManifests(); err != nil {
		return Manifest{}, err
	}
	return catalog.Get(name)
}

func Names() []string {
	manifests := List()
	names := make([]string, 0, len(manifests))
	for _, m := range manifests {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}

func Get(name string) (Adapter, bool) {
	if err := LoadExternalManifests(); err != nil {
		return Adapter{}, false
	}
	m, err := catalog.Get(strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return Adapter{}, false
	}
	return m.Adapter, true
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

func ShellPreview(name, notifyBin string, minMS int64) (shell.PreviewInfo, error) {
	return shell.Preview(strings.ToLower(strings.TrimSpace(name)), notifyBin, minMS)
}

func ShellBackupPath(ctx context.Context, db *store.DB, name string) string {
	return shell.BackupPath(ctx, db, strings.ToLower(strings.TrimSpace(name)))
}

func ShellNames() []string { return shell.Supported() }

func Unsupported(name string) error {
	return fmt.Errorf("adapter %q not supported; available: %s", name, strings.Join(Names(), ", "))
}
