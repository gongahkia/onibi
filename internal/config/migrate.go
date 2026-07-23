package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"gopkg.in/yaml.v3"
)

type MigrationResult struct {
	Path       string
	BackupPath string
	Changes    []string
}

func (r MigrationResult) Changed() bool { return len(r.Changes) > 0 }

func Migrate(paths Paths) (MigrationResult, error) {
	path := paths.Config
	if path == "" {
		path = filepath.Join(paths.StateDir, "config.yaml")
	}
	result := MigrationResult{Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	root, err := configMapping(b)
	if err != nil {
		return result, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, key := range []string{"voice", "workspace", "team"} {
		if mappingDelete(root, key) {
			result.Changes = append(result.Changes, "removed deprecated "+key+" configuration")
		}
	}
	if !result.Changed() {
		return result, nil
	}
	migrated, err := yaml.Marshal(root)
	if err != nil {
		return result, err
	}
	if _, _, err := loadBytes(path, migrated, Default(), LoadMeta{Path: path, Exists: true, Explicit: map[string]bool{}}); err != nil {
		return result, err
	}
	backup := path + ".pre-v1"
	if _, err := os.Stat(backup); err == nil {
		return result, fmt.Errorf("migration backup already exists: %s", backup)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err := os.WriteFile(backup, b, 0o600); err != nil {
		return result, err
	}
	if err := os.Chmod(backup, 0o600); err != nil {
		return result, err
	}
	if err := os.WriteFile(path+".tmp", append(migrated, '\n'), 0o600); err != nil {
		return result, err
	}
	if err := os.Chmod(path+".tmp", 0o600); err != nil {
		_ = os.Remove(path + ".tmp")
		return result, err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return result, err
	}
	result.BackupPath = backup
	return result, nil
}

func configMapping(b []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("config must be a YAML mapping")
	}
	return doc.Content[0], nil
}

func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, int) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], i
		}
	}
	return nil, -1
}

func mappingDelete(mapping *yaml.Node, key string) bool {
	_, index := mappingValue(mapping, key)
	if index < 0 {
		return false
	}
	mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
	return true
}
