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
	Argv0   string
}

type shellSpec struct {
	Name   string
	Binary string
}

func shellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell [shell]",
		Short: "Start your configured shell under Onibi PTY control",
		Long:  "Start your configured shell under Onibi PTY control.\n\nSupported shells: " + supportedShellList() + ".",
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
	if launch.Argv0 != "" {
		if err := cmd.Flags().Set("argv0", launch.Argv0); err != nil {
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
	base := strings.ToLower(filepath.Base(candidate))
	spec, ok := shellSpecFor(base)
	if !ok {
		return shellLaunch{}, fmt.Errorf("unsupported shell %q; use %s, or an absolute path to one of them", candidate, supportedShellList())
	}
	if strings.Contains(candidate, "/") && !filepath.IsAbs(candidate) {
		return shellLaunch{}, fmt.Errorf("shell path must be absolute: %s", candidate)
	}
	command := candidate
	if !strings.Contains(candidate, "/") {
		path, err := lookPath(spec.Binary)
		if err != nil {
			return shellLaunch{}, err
		}
		command = path
	}
	args, argv0 := shellStartup(spec.Name, login)
	return shellLaunch{Name: spec.Name, Command: command, Args: args, Argv0: argv0}, nil
}

func supportedShell(name string) bool {
	_, ok := shellSpecFor(name)
	return ok
}

func shellSpecFor(name string) (shellSpec, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "zsh":
		return shellSpec{Name: "zsh", Binary: "zsh"}, true
	case "bash":
		return shellSpec{Name: "bash", Binary: "bash"}, true
	case "fish":
		return shellSpec{Name: "fish", Binary: "fish"}, true
	case "sh":
		return shellSpec{Name: "sh", Binary: "sh"}, true
	case "nu", "nushell":
		return shellSpec{Name: "nu", Binary: "nu"}, true
	case "pwsh":
		return shellSpec{Name: "pwsh", Binary: "pwsh"}, true
	case "powershell":
		return shellSpec{Name: "powershell", Binary: "powershell"}, true
	case "ksh":
		return shellSpec{Name: "ksh", Binary: "ksh"}, true
	case "ksh93":
		return shellSpec{Name: "ksh93", Binary: "ksh93"}, true
	case "mksh":
		return shellSpec{Name: "mksh", Binary: "mksh"}, true
	case "oksh":
		return shellSpec{Name: "oksh", Binary: "oksh"}, true
	case "tcsh":
		return shellSpec{Name: "tcsh", Binary: "tcsh"}, true
	case "csh":
		return shellSpec{Name: "csh", Binary: "csh"}, true
	case "dash":
		return shellSpec{Name: "dash", Binary: "dash"}, true
	case "ash":
		return shellSpec{Name: "ash", Binary: "ash"}, true
	case "busybox", "busybox-sh":
		return shellSpec{Name: "busybox", Binary: "busybox"}, true
	default:
		return shellSpec{}, false
	}
}

func autoShellCandidates() []string {
	if runtime.GOOS == "darwin" {
		return []string{"zsh", "bash", "fish", "nu", "pwsh", "ksh", "mksh", "tcsh", "csh", "dash", "ash", "busybox", "sh"}
	}
	return []string{"bash", "zsh", "fish", "nu", "pwsh", "ksh", "mksh", "oksh", "tcsh", "csh", "dash", "ash", "busybox", "sh"}
}

func shellStartup(name string, login bool) ([]string, string) {
	switch name {
	case "zsh":
		if login {
			return []string{"-il"}, ""
		}
		return []string{"-i"}, ""
	case "bash":
		if login {
			return []string{"--login", "-i"}, ""
		}
		return []string{"-i"}, ""
	case "fish":
		if login {
			return []string{"--login", "--interactive"}, ""
		}
		return []string{"--interactive"}, ""
	case "nu":
		if login {
			return []string{"--login"}, ""
		}
		return nil, ""
	case "pwsh", "powershell":
		return []string{"-NoLogo"}, ""
	case "ksh", "ksh93", "mksh", "oksh":
		if login {
			return []string{"-l", "-i"}, ""
		}
		return []string{"-i"}, ""
	case "tcsh":
		if login {
			return []string{"-l", "-i"}, ""
		}
		return []string{"-i"}, ""
	case "csh":
		if login {
			return []string{"-i"}, "-csh"
		}
		return []string{"-i"}, ""
	case "dash", "ash", "sh":
		if login {
			return []string{"-i"}, "-" + name
		}
		return []string{"-i"}, ""
	case "busybox":
		if login {
			return []string{"-i"}, "-sh"
		}
		return []string{"-i"}, "sh"
	default:
		return []string{"-i"}, ""
	}
}

func shellStartupArgs(name string, login bool) []string {
	args, _ := shellStartup(name, login)
	return args
}

func supportedShellList() string {
	return "zsh, bash, fish, sh, nu, pwsh, powershell, ksh, ksh93, mksh, oksh, tcsh, csh, dash, ash, busybox"
}
