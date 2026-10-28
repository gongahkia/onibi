package service

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

type webRunner struct {
	calls    []call
	loaded   bool
	failStop bool
	pid      bool
}

func (r *webRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{name: name, args: args})
	switch args[0] {
	case "print":
		if !r.loaded {
			return []byte("Could not find service"), errors.New("missing")
		}
		if r.pid {
			return []byte("state = running\n pid = 42"), nil
		}
		return []byte("state = exited"), nil
	case "bootstrap":
		r.loaded = true
		r.pid = true
	case "bootout":
		if r.failStop {
			return []byte("operation not permitted"), errors.New("denied")
		}
		r.loaded = false
		r.pid = false
	}
	return nil, nil
}
func TestWebServiceLifecycleIsSeparateAndPreservesFailedStops(t *testing.T) {
	ctx := context.Background()
	r := &webRunner{}
	m := WebManager{&Manager{Paths: testPaths(t), Executable: "/Applications/My Tools/onibi & test", Home: t.TempDir(), UID: 501, GOOS: "darwin", Runner: r}}
	if err := m.Install(ctx, "127.0.0.1:8787", "https://mac.example.ts.net"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Fatal("install unexpectedly started service")
	}
	path, _ := m.path()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("invalid XML", err)
		}
	}
	for _, want := range []string{WebLabel, "web.out.log", "--background", "--origin", "https://mac.example.ts.net", "Aqua", "ExitTimeOut", "<integer>45</integer>", "onibi &amp; test"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(raw), Label) || strings.Contains(string(raw), "token=") {
		t.Fatal("web service shares daemon identity or logs secret")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Fatal("service configuration not private")
	}
	if err := m.Install(ctx, "127.0.0.1:8787", ""); err == nil {
		t.Fatal("silently replaced configuration")
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !m.Status(ctx).Running {
		t.Fatal("running process not detected")
	}
	r.pid = false
	if m.Status(ctx).Running {
		t.Fatal("loaded job incorrectly marked running")
	}
	r.failStop = true
	if err := m.Remove(ctx); err == nil {
		t.Fatal("ignored failed stop")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed stop removed configuration")
	}
	r.failStop = false
	if err := m.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if m.Status(ctx).Running {
		t.Fatal("stopped job still running")
	}
	if err := m.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("configuration retained")
	}
	for _, call := range r.calls {
		for _, arg := range call.args {
			if strings.Contains(arg, Label) || strings.Contains(arg, "system/") || arg == "-k" {
				t.Fatalf("unsafe lifecycle call %#v", call)
			}
		}
	}
}
func TestWebServiceRejectsUnsafeConfiguration(t *testing.T) {
	m := WebManager{&Manager{Paths: testPaths(t), Executable: "/bin/onibi", Home: t.TempDir(), UID: 501, GOOS: "darwin", Runner: &webRunner{}}}
	for _, address := range []string{"0.0.0.0:8787", "127.0.0.1:0", "127.0.0.1:bad", "127.0.0.1:65536", ":8787"} {
		if err := m.Install(context.Background(), address, ""); err == nil {
			t.Fatalf("accepted %s", address)
		}
	}
	if err := m.Install(context.Background(), "127.0.0.1:8787", "http://public.example"); err == nil {
		t.Fatal("accepted public HTTP")
	}
	m.GOOS = "windows"
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("accepted native web service on Windows")
	}
}
