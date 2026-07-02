package service

import (
	"context"
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
	for _, want := range []string{Label, "/usr/local/bin/onibi", "<string>run</string>", "RunAtLoad", "KeepAlive", "Crashed", "Interactive", "EnvironmentVariables", "/opt/homebrew/bin"} {
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
	for _, want := range []string{"ExecStart=\"/usr/local/bin/onibi\" run", "Restart=on-abnormal", "WantedBy=default.target"} {
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

func TestLaunchdPIDParsesPrintOutput(t *testing.T) {
	r := &fakeRunner{out: []byte("state = running\npid = 12345\n")}
	m := &Manager{
		Paths:      testPaths(t),
		Executable: "/usr/local/bin/onibi",
		Runner:     r,
		GOOS:       "darwin",
		Home:       t.TempDir(),
		UID:        501,
	}
	pid, ok, err := m.PID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pid != 12345 {
		t.Fatalf("pid=%d ok=%v", pid, ok)
	}
}

func TestSystemdPIDParsesMainPID(t *testing.T) {
	r := &fakeRunner{out: []byte("23456\n")}
	m := &Manager{
		Paths:      testPaths(t),
		Executable: "/usr/local/bin/onibi",
		Runner:     r,
		GOOS:       "linux",
		Home:       t.TempDir(),
		UID:        1000,
	}
	pid, ok, err := m.PID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pid != 23456 {
		t.Fatalf("pid=%d ok=%v", pid, ok)
	}
}
