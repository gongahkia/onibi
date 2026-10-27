package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	calls []call
	out   []byte
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	return f.out, f.err
}

func testPaths(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	return config.Paths{
		StateDir: filepath.Join(dir, "state"),
		Socket:   filepath.Join(dir, "state", "onibi.sock"),
		DBFile:   filepath.Join(dir, "state", "onibi.sqlite"),
		EnvFile:  filepath.Join(dir, "state", ".env"),
		LogDir:   filepath.Join(dir, "state", "logs"),
	}
}

func TestInstallLaunchdWritesPlistAndBootstraps(t *testing.T) {
	paths := testPaths(t)
	r := &fakeRunner{}
	m := &Manager{
		Paths:      paths,
		Executable: "/usr/local/bin/onibi",
		Runner:     r,
		GOOS:       "darwin",
		Home:       t.TempDir(),
		UID:        501,
	}
	if err := m.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, _ := m.ServicePath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{Label, "/usr/local/bin/onibi", "<string>start</string>", "RunAtLoad", "KeepAlive", "Crashed", "Interactive", "EnvironmentVariables", "/opt/homebrew/bin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("plist missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<key>KeepAlive</key>\n  <true/>") {
		t.Fatalf("plist restarts every failed exit:\n%s", body)
	}
	if len(r.calls) != 2 || r.calls[0].args[0] != "bootout" || r.calls[1].args[0] != "bootstrap" {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestInstallSystemdWritesUnitAndEnables(t *testing.T) {
	paths := testPaths(t)
	r := &fakeRunner{}
	m := &Manager{
		Paths:      paths,
		Executable: "/usr/local/bin/onibi",
		Runner:     r,
		GOOS:       "linux",
		Home:       t.TempDir(),
		UID:        1000,
	}
	if err := m.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, _ := m.ServicePath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, want := range []string{"ExecStart=\"/usr/local/bin/onibi\" start", "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(body, want) {
			t.Fatalf("unit missing %q:\n%s", want, body)
		}
	}
	if len(r.calls) != 2 || r.calls[0].args[1] != "daemon-reload" || r.calls[1].args[1] != "enable" {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestRestartLaunchdKickstartsAgent(t *testing.T) {
	r := &fakeRunner{}
	m := &Manager{
		Paths:      testPaths(t),
		Executable: "/usr/local/bin/onibi",
		Runner:     r,
		GOOS:       "darwin",
		Home:       t.TempDir(),
		UID:        501,
	}
	if err := m.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0].name != "launchctl" || strings.Join(r.calls[0].args, " ") != "kickstart -k gui/501/"+Label {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestRestartSystemdRestartsUserUnit(t *testing.T) {
	r := &fakeRunner{}
	m := &Manager{
		Paths:      testPaths(t),
		Executable: "/usr/local/bin/onibi",
		Runner:     r,
		GOOS:       "linux",
		Home:       t.TempDir(),
		UID:        1000,
	}
	if err := m.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0].name != "systemctl" || strings.Join(r.calls[0].args, " ") != "--user restart "+UnitName {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestLaunchdStatusDistinguishesLoadedAndRunning(t *testing.T) {
	for _, tt := range []struct {
		name    string
		output  string
		running bool
	}{
		{"running", "state = running\n\tpid = 12345\n", true},
		{"exited", "state = not running\nlast exit code = 0\n", false},
		{"waiting", "state = waiting\npid = 0\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manager{Paths: testPaths(t), Executable: "/usr/local/bin/onibi", Runner: &fakeRunner{out: []byte(tt.output)}, GOOS: "darwin", Home: t.TempDir(), UID: 501}
			if got := m.Status(t.Context()); got.Running != tt.running {
				t.Fatalf("running=%t want=%t detail=%q", got.Running, tt.running, got.Detail)
			}
		})
	}
}

func TestUninstallStopFailurePreservesServiceConfiguration(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			m := &Manager{Paths: testPaths(t), Executable: "/usr/local/bin/onibi", Runner: &fakeRunner{out: []byte("permission denied"), err: errors.New("stop failed")}, GOOS: platform, Home: t.TempDir(), UID: 501}
			path, err := m.ServicePath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := m.Uninstall(t.Context()); err == nil {
				t.Fatal("ignored stop failure")
			}
			if raw, err := os.ReadFile(path); err != nil || string(raw) != "keep" {
				t.Fatal("service file lost")
			}
		})
	}
}
