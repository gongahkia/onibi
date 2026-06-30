package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/catalog"
)

func LoadExternalManifests() error {
	dir, err := ExternalManifestDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		manifest, err := catalog.ParseManifest(body, path)
		if err != nil {
			return err
		}
		if _, err := catalog.Get(manifest.Name); err == nil {
			continue
		}
		if err := catalog.Register(manifest); err != nil {
			return err
		}
	}
	return nil
}

func ExternalManifestDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("ONIBI_ADAPTERS_DIR")); dir != "" {
		return filepath.Abs(dir)
	}
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "onibi", "adapters"), nil
}
