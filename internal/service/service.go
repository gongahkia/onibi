package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
)

const (
	Label    = "sh.onibi.daemon"
	UnitName = "onibi.service"
)

// Runner executes service-manager commands.
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// ExecRunner uses os/exec.
type ExecRunner struct{}

// Run executes name with args and returns combined output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Manager installs, uninstalls, and inspects the user service.
type Manager struct {
	Paths      config.Paths
	Executable string
	Runner     Runner
	GOOS       string
	Home       string
	UID        int
}

// Status is the service manager state visible to doctor.
type Status struct {
	Path      string
	Installed bool
	Running   bool
	Detail    string
}

// NewManager returns a Manager using current-user defaults.
func NewManager(paths config.Paths, executable string) (*Manager, error) {
	if executable == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
		executable = exe
	}
	abs, err := filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Manager{
		Paths:      paths,
		Executable: abs,
		Runner:     ExecRunner{},
		GOOS:       runtime.GOOS,
		Home:       home,
		UID:        os.Getuid(),
	}, nil
}

// Install writes the platform service file and starts/enables it.
func (m *Manager) Install(ctx context.Context) error {
	if err := m.ensure(); err != nil {
		return err
	}
	switch m.GOOS {
	case "darwin":
		return m.installLaunchd(ctx)
	case "linux":
		return m.installSystemd(ctx)
	default:
		return fmt.Errorf("unsupported os: %s", m.GOOS)
	}
}

// Uninstall stops/disables the platform service and removes its file.
func (m *Manager) Uninstall(ctx context.Context) error {
	if err := m.ensure(); err != nil {
		return err
	}
	switch m.GOOS {
	case "darwin":
		return m.uninstallLaunchd(ctx)
	case "linux":
		return m.uninstallSystemd(ctx)
	default:
		return fmt.Errorf("unsupported os: %s", m.GOOS)
	}
}

func (m *Manager) Restart(ctx context.Context) error {
	if err := m.ensure(); err != nil {
		return err
	}
	switch m.GOOS {
	case "darwin":
		return m.restartLaunchd(ctx)
	case "linux":
		return m.restartSystemd(ctx)
	default:
		return fmt.Errorf("unsupported os: %s", m.GOOS)
	}
}

func (m *Manager) PID(ctx context.Context) (int, bool, error) {
	if err := m.ensure(); err != nil {
		return 0, false, err
	}
	switch m.GOOS {
	case "darwin":
		return m.launchdPID(ctx)
	case "linux":
		return m.systemdPID(ctx)
	default:
		return 0, false, fmt.Errorf("unsupported os: %s", m.GOOS)
	}
}

// Status reports whether the service file exists and appears to be running.
func (m *Manager) Status(ctx context.Context) Status {
	if err := m.ensure(); err != nil {
		return Status{Detail: err.Error()}
	}
	switch m.GOOS {
	case "darwin":
		return m.launchdStatus(ctx)
	case "linux":
		return m.systemdStatus(ctx)
	default:
		return Status{Detail: "unsupported os: " + m.GOOS}
	}
}

func parsePID(raw string) (int, bool) {
	pid, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// ServicePath returns the service-file path for the manager platform.
func (m *Manager) ServicePath() (string, error) {
	if err := m.ensure(); err != nil {
		return "", err
	}
	switch m.GOOS {
	case "darwin":
		return filepath.Join(m.Home, "Library", "LaunchAgents", Label+".plist"), nil
	case "linux":
		return filepath.Join(m.Home, ".config", "systemd", "user", UnitName), nil
	default:
		return "", fmt.Errorf("unsupported os: %s", m.GOOS)
	}
}

func (m *Manager) ensure() error {
	if m.Runner == nil {
		m.Runner = ExecRunner{}
	}
	if m.GOOS == "" {
		m.GOOS = runtime.GOOS
	}
	if m.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		m.Home = home
	}
	if m.UID == 0 {
		m.UID = os.Getuid()
	}
	if m.Executable == "" {
		return errors.New("service executable required")
	}
	abs, err := filepath.Abs(m.Executable)
	if err != nil {
		return err
	}
	m.Executable = abs
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
