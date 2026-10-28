package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type webSystemdRunner struct {
	calls   []call
	running bool
	fail    string
}

func (r *webSystemdRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{name, append([]string(nil), args...)})
	if len(args) < 2 || args[0] != "--user" {
		return nil, errors.New("expected user manager only")
	}
	if args[1] == r.fail {
		return []byte("manager unavailable"), errors.New("failed")
	}
	switch args[1] {
	case "start":
		r.running = true
	case "stop", "disable":
		r.running = false
	case "is-active":
		if r.running {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), errors.New("inactive")
	}
	return nil, nil
}

func TestWebSystemdLifecycleAndSessionSurvivalPolicy(t *testing.T) {
	r := &webSystemdRunner{}
	m := WebManager{&Manager{Paths: testPaths(t), Executable: "/tmp/spaced %n $HOME/onibi", Home: t.TempDir(), GOOS: "linux", Runner: r}}
	t.Setenv("PATH", "/tmp/tools with spaces/%n:$HOME:/usr/bin")
	t.Setenv("XDG_DATA_HOME", "/tmp/private data")
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/private runtime")
	if err := m.Install(t.Context(), "127.0.0.1:8787", "https://host.example.ts.net"); err != nil {
		t.Fatal(err)
	}
	path, err := m.path()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ExecStart=:"/tmp/spaced %%n $HOME/onibi" web --background`,
		`Environment="PATH=/tmp/tools with spaces/%%n:$HOME:/usr/bin"`,
		`Environment="XDG_DATA_HOME=/tmp/private data"`,
		`Environment="XDG_RUNTIME_DIR=/tmp/private runtime"`,
		"Restart=on-failure", "TimeoutStopSec=60", "KillMode=process", "UMask=0077", "WantedBy=default.target",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "token=") {
		t.Fatal("pairing secrets in unit")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Fatal("unit is not private")
	}
	want := []call{{"systemctl", []string{"--user", "show", "--property=Version", "--value"}}, {"systemctl", []string{"--user", "daemon-reload"}}, {"systemctl", []string{"--user", "enable", WebUnitName}}}
	if !reflect.DeepEqual(r.calls, want) || r.running {
		t.Fatalf("install unexpectedly starts service: %#v", r.calls)
	}
	if err := m.Install(t.Context(), "127.0.0.1:8787", ""); err == nil {
		t.Fatal("overwrote unit")
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !m.Status(t.Context()).Running {
		t.Fatal("running service reported stopped")
	}
	if err := m.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.fail = "disable"
	if err := m.Remove(t.Context()); err == nil {
		t.Fatal("ignored failed stop/disable")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed stop removed unit")
	}
	r.fail = ""
	if err := m.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.Status(t.Context()).Running {
		t.Fatal("stopped service reported running")
	}
	if err := m.Remove(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unit remains")
	}
	if _, err := os.Stat(m.Paths.StateDir); err != nil {
		t.Fatal("state removed")
	}
	for _, c := range r.calls {
		for _, arg := range c.args {
			if arg == UnitName || arg == "--system" || arg == "--now" && c.args[1] == "enable" {
				t.Fatalf("unsafe service call: %#v", c)
			}
		}
	}
}

func TestWebSystemdUnavailableDoesNotInstallUnit(t *testing.T) {
	r := &webSystemdRunner{fail: "show"}
	m := WebManager{&Manager{Paths: testPaths(t), Executable: "/bin/onibi", Home: t.TempDir(), ConfigHome: t.TempDir(), GOOS: "linux", Runner: r}}
	err := m.Install(t.Context(), "127.0.0.1:8787", "")
	if err == nil || !strings.Contains(err.Error(), "WSL") || !strings.Contains(err.Error(), "foreground") {
		t.Fatalf("unhelpful failure: %v", err)
	}
	path, _ := m.path()
	if path != filepath.Join(m.ConfigHome, "systemd", "user", WebUnitName) {
		t.Fatal("ignored XDG config root")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unit written without user manager")
	}
}

func TestWebSystemdUnitParsesWithInstalledSystemd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd is Linux-only")
	}
	bin, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not installed")
	}
	m := WebManager{&Manager{Paths: testPaths(t), Executable: "/bin/true", Home: t.TempDir(), GOOS: "linux"}}
	path := filepath.Join(t.TempDir(), WebUnitName)
	if err := os.WriteFile(path, []byte(m.webSystemdUnit("127.0.0.1:8787", "http://127.0.0.1:8787")), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), bin, "verify", path).CombinedOutput(); err != nil {
		t.Fatalf("systemd rejected generated unit: %v\n%s", err, out)
	}
}
