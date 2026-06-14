package cli

import (
	"errors"
	"testing"
)

func TestResolveShellLaunchAutoUsesShellEnv(t *testing.T) {
	launch, err := resolveShellLaunch("auto", true, func(key string) string {
		if key == "SHELL" {
			return "/bin/zsh"
		}
		return ""
	}, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "zsh" || launch.Command != "/bin/zsh" || len(launch.Args) != 1 || launch.Args[0] != "-il" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchExplicitFishNoLogin(t *testing.T) {
	launch, err := resolveShellLaunch("fish", false, func(string) string { return "" }, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "fish" || launch.Command != "/usr/bin/fish" || len(launch.Args) != 1 || launch.Args[0] != "--interactive" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchRejectsUnsupported(t *testing.T) {
	_, err := resolveShellLaunch("elvish", true, func(string) string { return "" }, fakeLookPath)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveShellLaunchNuLogin(t *testing.T) {
	launch, err := resolveShellLaunch("nu", true, func(string) string { return "" }, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "nu" || launch.Command != "/usr/bin/nu" || len(launch.Args) != 1 || launch.Args[0] != "--login" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchPwshIgnoresLogin(t *testing.T) {
	launch, err := resolveShellLaunch("pwsh", true, func(string) string { return "" }, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "pwsh" || launch.Command != "/usr/bin/pwsh" || len(launch.Args) != 1 || launch.Args[0] != "-NoLogo" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchCshLoginUsesArgv0(t *testing.T) {
	launch, err := resolveShellLaunch("csh", true, func(string) string { return "" }, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "csh" || launch.Argv0 != "-csh" || len(launch.Args) != 1 || launch.Args[0] != "-i" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchDashLoginUsesArgv0(t *testing.T) {
	launch, err := resolveShellLaunch("dash", true, func(string) string { return "" }, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "dash" || launch.Argv0 != "-dash" || len(launch.Args) != 1 || launch.Args[0] != "-i" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchBusybox(t *testing.T) {
	launch, err := resolveShellLaunch("busybox-sh", false, func(string) string { return "" }, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Name != "busybox" || launch.Command != "/usr/bin/busybox" || launch.Argv0 != "sh" || len(launch.Args) != 1 || launch.Args[0] != "-i" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestResolveShellLaunchRejectsRelativePath(t *testing.T) {
	_, err := resolveShellLaunch("./zsh", true, func(string) string { return "" }, fakeLookPath)
	if err == nil {
		t.Fatal("expected error")
	}
}

func fakeLookPath(name string) (string, error) {
	switch name {
	case "zsh", "bash", "fish", "sh", "nu", "pwsh", "powershell",
		"ksh", "ksh93", "mksh", "oksh", "tcsh", "csh", "dash", "ash", "busybox":
		return "/usr/bin/" + name, nil
	default:
		return "", errors.New("not found")
	}
}
