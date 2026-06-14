package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
)

type shellLaunch struct {
	Name    string
	Command string
	Args    []string
}

func shellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell [shell]",
		Short: "Start your configured shell under Onibi PTY control",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runShell,
	}
	addRunFlags(cmd)
	cmd.Flags().Bool("login", false, "force login+interactive shell")
	cmd.Flags().Bool("no-login", false, "force interactive non-login shell")
	return cmd
}

func runShell(cmd *cobra.Command, args []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(paths)
	if err != nil {
		return err
	}
	kind := cfg.Shell.Default
	if len(args) > 0 {
		kind = args[0]
	}
	login := cfg.Shell.Login
	loginFlag, _ := cmd.Flags().GetBool("login")
	noLoginFlag, _ := cmd.Flags().GetBool("no-login")
	if loginFlag && noLoginFlag {
		return errors.New("--login and --no-login cannot be combined")
	}
	if cmd.Flags().Changed("login") {
		login = true
	}
	if cmd.Flags().Changed("no-login") {
		login = false
	}
	launch, err := resolveShellLaunch(kind, login, os.Getenv, exec.LookPath)
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("name") {
		if err := cmd.Flags().Set("name", launch.Name); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "using %s shell: %s %s\n", launch.Name, launch.Command, strings.Join(launch.Args, " "))
	return runRun(cmd, append([]string{launch.Command}, launch.Args...))
}

func resolveShellLaunch(kind string, login bool, getenv func(string) string, lookPath func(string) (string, error)) (shellLaunch, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" || strings.EqualFold(kind, "auto") {
		if sh := strings.TrimSpace(getenv("SHELL")); sh != "" {
			if launch, err := shellLaunchFor(sh, login, lookPath); err == nil {
				return launch, nil
			}
		}
		for _, candidate := range autoShellCandidates() {
			if launch, err := shellLaunchFor(candidate, login, lookPath); err == nil {
				return launch, nil
			}
		}
		return shellLaunch{}, errors.New("no supported shell found; set shell.default to zsh, bash, fish, sh, or an absolute path")
	}
	return shellLaunchFor(kind, login, lookPath)
}

func shellLaunchFor(candidate string, login bool, lookPath func(string) (string, error)) (shellLaunch, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return shellLaunch{}, errors.New("shell required")
	}
	name := strings.ToLower(filepath.Base(candidate))
	if !supportedShell(name) {
		return shellLaunch{}, fmt.Errorf("unsupported shell %q; use zsh, bash, fish, sh, or an absolute path to one of them", candidate)
	}
	if strings.Contains(candidate, "/") && !filepath.IsAbs(candidate) {
		return shellLaunch{}, fmt.Errorf("shell path must be absolute: %s", candidate)
	}
	command := candidate
	if !strings.Contains(candidate, "/") {
		path, err := lookPath(candidate)
		if err != nil {
			return shellLaunch{}, err
		}
		command = path
	}
	return shellLaunch{Name: name, Command: command, Args: shellStartupArgs(name, login)}, nil
}

func supportedShell(name string) bool {
	switch name {
	case "zsh", "bash", "fish", "sh":
		return true
	default:
		return false
	}
}

func autoShellCandidates() []string {
	if runtime.GOOS == "darwin" {
		return []string{"zsh", "bash", "fish", "sh"}
	}
	return []string{"bash", "zsh", "fish", "sh"}
}

func shellStartupArgs(name string, login bool) []string {
	switch name {
	case "zsh":
		if login {
			return []string{"-il"}
		}
		return []string{"-i"}
	case "bash":
		if login {
			return []string{"--login", "-i"}
		}
		return []string{"-i"}
	case "fish":
		if login {
			return []string{"--login", "--interactive"}
		}
		return []string{"--interactive"}
	default:
		return []string{"-i"}
	}
}
