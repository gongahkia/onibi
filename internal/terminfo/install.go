package terminfo

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const XtermGhostty = "xterm-ghostty"

var (
	ErrMissingTIC  = errors.New("missing tic")
	lookPath       = exec.LookPath
	commandContext = exec.CommandContext
	userHomeDir    = os.UserHomeDir
)

//go:embed xterm-ghostty.terminfo
var xtermGhosttySource string

func Source() string {
	return xtermGhosttySource
}

func EnsureXtermGhostty(ctx context.Context) (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for terminfo install: %w", err)
	}
	return EnsureXtermGhosttyAt(ctx, home)
}

func EnsureXtermGhosttyAt(ctx context.Context, home string) (string, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return "", errors.New("resolve home for terminfo install: empty home")
	}
	if path, ok := InstalledPath(home); ok {
		return ensurePrimaryInstallPath(home, path)
	}
	tic, err := lookPath("tic")
	if err != nil {
		return "", fmt.Errorf("%w: xterm-ghostty terminfo requires `tic`; install ncurses (`brew install ncurses`) and ensure `tic` is on PATH", ErrMissingTIC)
	}
	dest := filepath.Join(home, ".terminfo")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", fmt.Errorf("create terminfo dir: %w", err)
	}
	src, err := os.CreateTemp("", "onibi-xterm-ghostty-*.terminfo")
	if err != nil {
		return "", fmt.Errorf("create terminfo source: %w", err)
	}
	srcPath := src.Name()
	defer os.Remove(srcPath)
	if _, err := src.WriteString(xtermGhosttySource); err != nil {
		_ = src.Close()
		return "", fmt.Errorf("write terminfo source: %w", err)
	}
	if err := src.Close(); err != nil {
		return "", fmt.Errorf("close terminfo source: %w", err)
	}
	cmd := commandContext(ctx, tic, "-x", "-o", dest, srcPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("compile xterm-ghostty terminfo: %w: %s", err, msg)
		}
		return "", fmt.Errorf("compile xterm-ghostty terminfo: %w", err)
	}
	if path, ok := InstalledPath(home); ok {
		return ensurePrimaryInstallPath(home, path)
	}
	return "", fmt.Errorf("compile xterm-ghostty terminfo: tic completed but %s was not created", PrimaryInstallPath(home))
}

func ensurePrimaryInstallPath(home, installed string) (string, error) {
	primary := PrimaryInstallPath(home)
	if installed == primary {
		return primary, nil
	}
	data, err := os.ReadFile(installed)
	if err != nil {
		return "", fmt.Errorf("read compiled terminfo: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(primary), 0o700); err != nil {
		return "", fmt.Errorf("create primary terminfo dir: %w", err)
	}
	if err := os.WriteFile(primary, data, 0o600); err != nil {
		return "", fmt.Errorf("write primary terminfo: %w", err)
	}
	return primary, nil
}

func InstalledPath(home string) (string, bool) {
	for _, path := range CandidateInstallPaths(home) {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, true
		}
	}
	return "", false
}

func PrimaryInstallPath(home string) string {
	return filepath.Join(home, ".terminfo", "x", XtermGhostty)
}

func CandidateInstallPaths(home string) []string {
	base := filepath.Join(home, ".terminfo")
	return []string{
		filepath.Join(base, "x", XtermGhostty),
		filepath.Join(base, "78", XtermGhostty),
	}
}
