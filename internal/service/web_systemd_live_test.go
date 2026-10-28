package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/webadmin"
)

// Opt-in: uses a uniquely named, runtime-only user unit and a private tmux
// socket. Never installs, starts or stops the user's onibi-web.service.
func TestWebSystemdLiveSessionSurvivesStop(t *testing.T) {
	if os.Getenv("ONIBI_SYSTEMD_SMOKE") != "1" {
		t.Skip("set ONIBI_SYSTEMD_SMOKE=1 to exercise a real systemd user manager")
	}
	if runtime.GOOS != "linux" {
		t.Skip("systemd is Linux-only")
	}
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal("live service test requires tmux", err)
	}
	managerEnv := os.Environ()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	ctl := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
		cmd.Env = managerEnv
		return cmd.CombinedOutput()
	}
	if out, err := ctl(ctx, "show", "--property=Version", "--value"); err != nil {
		t.Fatalf("user manager unavailable: %v %s", err, out)
	}
	root, err := os.MkdirTemp("", "onibi-svc-") // keep Unix socket paths short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	unit := filepath.Base(root) + ".service"
	if out, _ := ctl(ctx, "show", unit, "--property=LoadState", "--value"); strings.TrimSpace(string(out)) != "not-found" {
		t.Fatalf("refusing to touch an existing test unit %s: %s", unit, out)
	}
	for _, key := range []string{"XDG_DATA_HOME", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME"} {
		dir := filepath.Join(root, key)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	t.Setenv("TMUX", "")
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(root, "tmux-test")
	socket := filepath.Join(root, "tmux.sock")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+quote(tmux)+" -S "+quote(socket)+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, tmux, "-S", socket, "kill-server").Run()
	})
	cfg := config.Default()
	cfg.Multiplexer.Tmux = config.MuxBackend{Bin: wrapper, Config: "/dev/null"}
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "onibi")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/onibi")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	m := WebManager{&Manager{Paths: paths, Executable: binary}}
	path := filepath.Join(root, unit)
	if err := os.WriteFile(path, []byte(m.webSystemdUnit("127.0.0.1:0", "")), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := ctl(ctx, "link", "--runtime", path); err != nil {
		t.Fatalf("link test-only unit: %v %s", err, out)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, args := range [][]string{{"stop", unit}, {"disable", "--runtime", unit}, {"reset-failed", unit}, {"daemon-reload"}} {
			_, _ = ctl(cleanup, args...)
		}
	})
	waitForDaemon := func() {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			response, err := webadmin.Call(ctx, webadmin.Path(paths.StateDir), webadmin.Request{Operation: "status"})
			if err == nil && response.Status != nil && response.Status.SessionsOnly {
				if ping, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypePing}, time.Second); err == nil && ping.Reason == "" {
					return
				}
			}
		}
		status, _ := ctl(ctx, "status", unit, "--no-pager")
		t.Fatalf("web service did not become ready: %s", status)
	}
	if out, err := ctl(ctx, "start", unit); err != nil {
		t.Fatalf("start: %v %s", err, out)
	}
	waitForDaemon()
	session, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypeSessionNew, Agent: "/bin/sh", Args: []string{"-c", "printf SERVICE_READY; exec sleep 120"}, Name: "survives-stop", CWD: root, Mux: "tmux"}, 5*time.Second)
	if err != nil || session.Reason != "" {
		t.Fatalf("create session: %v %#v", err, session)
	}
	if out, err := ctl(ctx, "stop", unit); err != nil {
		t.Fatalf("stop: %v %s", err, out)
	}
	if out, err := exec.CommandContext(ctx, wrapper, "has-session", "-t", "onibi-"+session.SessionID).CombinedOutput(); err != nil {
		t.Fatalf("stopping the service killed its session: %v %s", err, out)
	}
	if out, err := ctl(ctx, "start", unit); err != nil {
		t.Fatalf("restart: %v %s", err, out)
	}
	waitForDaemon()
	listed, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypeSessionList}, 5*time.Second)
	if err != nil || len(listed.Sessions) != 1 || listed.Sessions[0].ID != session.SessionID {
		t.Fatalf("session not restored: %v %#v", err, listed)
	}
	ended, err := intake.Request(paths.Socket, intake.Event{Type: intake.TypeSessionControl, Session: session.SessionID, Action: "kill", Exact: true}, 5*time.Second)
	if err != nil || ended.Reason != "" {
		t.Fatalf("cleanup session: %v %#v", err, ended)
	}
	t.Log("Generated user unit starts the real Linux web server; tmux survives stop and restores after restart.")
}
