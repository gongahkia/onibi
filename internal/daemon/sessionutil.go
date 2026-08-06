package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func normalizeSessionCWD(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", cwd)
	}
	return resolved, nil
}

func (d *Daemon) agentCommand(agent string, args []string) (string, string, []string, bool) {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "shell" {
		shell := d.ShellDefault
		if shell == "" || shell == "auto" {
			shell = defaultShell()
		}
		bin, argv, ok := shellCommand(shell, args, d.ShellLogin)
		return bin, "shell", argv, ok
	}
	if agent == "pi" {
		return agentBinary("pi"), "pi", args, true
	}
	return "", "", nil, false
}
func agentBinary(name string) string {
	if v := strings.TrimSpace(os.Getenv("ONIBI_" + strings.ToUpper(name) + "_BIN")); v != "" {
		return v
	}
	return name
}
func defaultShell() string {
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return filepath.Base(s)
	}
	if runtime.GOOS == "darwin" {
		return "zsh"
	}
	return "bash"
}
func shellCommand(shell string, extra []string, login bool) (string, []string, bool) {
	shell = strings.ToLower(filepath.Base(shell))
	switch shell {
	case "zsh", "bash", "sh", "dash", "ash":
		args := []string{"-i"}
		if login {
			args = []string{"-l", "-i"}
		}
		return shell, append(args, extra...), true
	case "fish":
		return shell, append([]string{"--interactive"}, extra...), true
	case "nu", "pwsh", "powershell", "ksh", "ksh93", "mksh", "oksh", "tcsh", "csh":
		return shell, extra, true
	default:
		return "", nil, false
	}
}
