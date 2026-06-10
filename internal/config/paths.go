package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths resolves all the directories and file paths Onibi uses, applying
// platform conventions. On macOS the state dir lives under
// ~/Library/Application Support/onibi; on Linux it follows XDG.
type Paths struct {
	StateDir string // owns sqlite db, .env fallback, audit log
	Socket   string // intake socket (0600)
	DBFile   string // onibi.sqlite
	EnvFile  string // .env (fallback when no keystore)
	LogDir   string // rotated log files
	Config   string // config.yaml
}

// DefaultPaths returns the canonical paths for the current OS and user.
// It does not create any directories — callers should call EnsureDirs.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home: %w", err)
	}
	var state string
	switch runtime.GOOS {
	case "darwin":
		state = filepath.Join(home, "Library", "Application Support", "onibi")
	case "linux":
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			state = filepath.Join(x, "onibi")
		} else {
			state = filepath.Join(home, ".local", "share", "onibi")
		}
	default:
		return Paths{}, fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}
	socket := filepath.Join(state, "onibi.sock")
	if runtime.GOOS == "linux" {
		if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
			socket = filepath.Join(r, "onibi.sock")
		}
	}
	return Paths{
		StateDir: state,
		Socket:   socket,
		DBFile:   filepath.Join(state, "onibi.sqlite"),
		EnvFile:  filepath.Join(state, ".env"),
		LogDir:   filepath.Join(state, "logs"),
		Config:   filepath.Join(state, "config.yaml"),
	}, nil
}

// EnsureDirs creates the state directory tree with strict permissions (0700).
// Refuses to widen permissions on an existing dir — a loose state dir is a
// security smell we want the user to fix manually.
func (p Paths) EnsureDirs() error {
	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return fmt.Errorf("mkdir state: %w", err)
	}
	if err := enforceDirPerms(p.StateDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(p.LogDir, 0o700); err != nil {
		return fmt.Errorf("mkdir log: %w", err)
	}
	return enforceDirPerms(p.LogDir, 0o700)
}

func enforceDirPerms(dir string, want os.FileMode) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	got := fi.Mode().Perm()
	if got&^want != 0 {
		return fmt.Errorf("%s has loose permissions %#o (want %#o); fix with: chmod %#o %s", dir, got, want, want, dir)
	}
	return nil
}

// EnvFilePerms ensures the .env fallback file has 0600 perms when present.
func (p Paths) EnvFilePerms() error {
	fi, err := os.Stat(p.EnvFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().Perm() != 0o600 {
		return fmt.Errorf("%s has perms %#o (want 0600); fix with: chmod 0600 %s", p.EnvFile, fi.Mode().Perm(), p.EnvFile)
	}
	return nil
}
