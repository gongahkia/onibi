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

// agentCommand resolves what `session new` launches: "shell" is the
// configured shell, and anything else is a program run exactly as given.
// Agents such as codex, claude and pi get no settings, hooks or flag checks;
// the cockpit handles them like any other terminal program.
func (d *Daemon) agentCommand(program string, args []string) (string, string, []string, bool) {
	program = strings.TrimSpace(program)
	if strings.EqualFold(program, "shell") {
		shell := d.ShellDefault
		if shell == "" || shell == "auto" {
			shell = defaultShell()
		}
		bin, argv, ok := shellCommand(shell, args, d.ShellLogin)
		return bin, "shell", argv, ok
	}
	// The daemon's working directory is unrelated to the caller's, so a
	// relative path would be ambiguous.
	if program == "" || strings.HasPrefix(program, "-") || strings.ContainsAny(program, "\x00\r\n") ||
		(strings.ContainsRune(program, '/') && !filepath.IsAbs(program)) {
		return "", "", nil, false
	}
	return programBinary(program), programLabel(program), args, true
}
func programBinary(name string) string {
	if !strings.ContainsRune(name, '/') {
		if v := strings.TrimSpace(os.Getenv("ONIBI_" + strings.ToUpper(name) + "_BIN")); v != "" {
			return v
		}
	}
	return name
}

// programLabel is the session's program name and default session name, so it
// keeps only characters valid in session names.
func programLabel(program string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(filepath.Base(program)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() == 32 {
			break
		}
	}
	if b.Len() == 0 {
		return "program"
	}
	return b.String()
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
