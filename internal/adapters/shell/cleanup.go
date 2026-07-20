// Package shell removes legacy Onibi shell lifecycle hooks.
package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	begin = "# >>> onibi managed shell hook"
	end   = "# <<< onibi managed shell hook"
)

// CleanupLegacy removes only complete Onibi-managed shell hook blocks.
func CleanupLegacy() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	paths := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".config", "fish", "conf.d", "onibi.fish"),
	}
	removed := 0
	for _, path := range paths {
		ok, err := removeManaged(path)
		if err != nil {
			return removed, err
		}
		if ok {
			removed++
		}
	}
	return removed, nil
}

func removeManaged(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	clean, ok := removeBlockText(string(b))
	if !ok {
		return false, nil
	}
	if strings.HasSuffix(path, string(filepath.Separator)+"onibi.fish") && strings.TrimSpace(clean) == "" {
		return true, os.Remove(path)
	}
	return true, os.WriteFile(path, []byte(clean), 0o600)
}

func removeBlockText(src string) (string, bool) {
	clean := src
	removed := false
	for {
		i := strings.Index(clean, begin)
		if i < 0 {
			return clean, removed
		}
		j := strings.Index(clean[i:], end)
		if j < 0 {
			return clean, removed
		}
		j = i + j + len(end)
		if j < len(clean) && clean[j] == '\n' {
			j++
		}
		clean = clean[:i] + clean[j:]
		removed = true
	}
}
