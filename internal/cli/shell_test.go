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
	_, err := resolveShellLaunch("pwsh", true, func(string) string { return "" }, fakeLookPath)
	if err == nil {
		t.Fatal("expected error")
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
	case "zsh", "bash", "fish", "sh":
		return "/usr/bin/" + name, nil
	default:
		return "", errors.New("not found")
	}
}
