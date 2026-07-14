package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
	sshtransport "github.com/gongahkia/onibi/internal/transport/ssh"
)

func TestParseSSHTarget(t *testing.T) {
	got, err := parseSSHTarget("pi@raspberrypi.local:2222")
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "pi" || got.Host != "raspberrypi.local:2222" {
		t.Fatalf("target = %#v", got)
	}
	if _, err := parseSSHTarget("raspberrypi.local"); err == nil {
		t.Fatal("expected missing user error")
	}
}

func TestBootstrapSSHOrchestratesRemoteInstallServiceTunnelAndPair(t *testing.T) {
	oldReadKey := readSSHKey
	oldConnect := connectSSHClient
	fake := &fakeSSHRemote{
		platform: sshtransport.Platform{GOOS: "linux", GOARCH: "arm64"},
		tunnel:   fakeSSHTunnel{url: "https://127.0.0.1:18443"},
		runOut:   "https://127.0.0.1:18443/pair/tok\n",
	}
	readSSHKey = func(*cobra.Command) ([]byte, error) { return []byte("key"), nil }
	connectSSHClient = func(target sshTarget, key []byte, _ *cobra.Command) (sshRemoteClient, error) {
		if target.User != "pi" || target.Host != "host" || string(key) != "key" {
			t.Fatalf("target=%#v key=%q", target, key)
		}
		return fake, nil
	}
	t.Cleanup(func() {
		readSSHKey = oldReadKey
		connectSSHClient = oldConnect
	})
	cmd := upCmd()
	res, tunnel, err := bootstrapSSH(context.Background(), cmd, "pi@host")
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	if res.PairURL != "https://127.0.0.1:18443/pair/tok" {
		t.Fatalf("pair url = %q", res.PairURL)
	}
	want := []string{"detect", "install", "service", "tunnel", "run:$HOME/.local/bin/onibi pair --host 127.0.0.1 --port 18443 --no-qr --quiet"}
	if strings.Join(fake.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestUpSSHDispatchesBeforeWebPair(t *testing.T) {
	withDefaultState(t)
	oldSSHUp := sshUpRun
	oldWebPair := webPairRun
	called := false
	sshUpRun = func(cmd *cobra.Command, target string) error {
		called = true
		if target != "pi@host" {
			t.Fatalf("target = %q", target)
		}
		return nil
	}
	webPairRun = func(*cobra.Command, config.Paths, *store.DB) error {
		t.Fatal("web pair should not run")
		return nil
	}
	t.Cleanup(func() {
		sshUpRun = oldSSHUp
		webPairRun = oldWebPair
	})
	cmd := upCmd()
	cmd.SetArgs([]string{"--ssh", "pi@host"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("ssh up not called")
	}
}

func TestMintRemotePairURLRejectsBadTunnelURL(t *testing.T) {
	if _, err := mintRemotePairURL(&fakeSSHRemote{}, "https://127.0.0.1"); err == nil {
		t.Fatal("expected missing port error")
	}
}

func TestSSHStatusAndTeardownExerciseRemoteLifecycle(t *testing.T) {
	oldReadKey := readSSHKey
	oldConnect := connectSSHClient
	fake := &fakeSSHRemote{platform: sshtransport.Platform{GOOS: "linux", GOARCH: "arm64"}}
	readSSHKey = func(*cobra.Command) ([]byte, error) { return []byte("key"), nil }
	connectSSHClient = func(sshTarget, []byte, *cobra.Command) (sshRemoteClient, error) { return fake, nil }
	t.Cleanup(func() {
		readSSHKey = oldReadKey
		connectSSHClient = oldConnect
	})
	status := sshCmd()
	var out strings.Builder
	status.SetOut(&out)
	if err := runSSHStatus(status, []string{"pi@host"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "active\n" {
		t.Fatalf("status output = %q", out.String())
	}
	teardown := sshCmd()
	teardown.SetOut(&out)
	if err := runSSHTeardown(teardown, []string{"pi@host"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SSH remote torn down") {
		t.Fatalf("teardown output = %q", out.String())
	}
	if got, want := strings.Join(fake.calls, "|"), "detect|status|close|detect|teardown|close"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

type fakeSSHRemote struct {
	platform sshtransport.Platform
	tunnel   fakeSSHTunnel
	runOut   string
	calls    []string
}

func (f *fakeSSHRemote) DetectArch() (sshtransport.Platform, error) {
	f.calls = append(f.calls, "detect")
	return f.platform, nil
}

func (f *fakeSSHRemote) InstallBinaries(sshtransport.Platform, sshtransport.InstallOptions) (sshtransport.InstallResult, error) {
	f.calls = append(f.calls, "install")
	return sshtransport.InstallResult{}, nil
}

func (f *fakeSSHRemote) InstallService(sshtransport.Platform, sshtransport.ServiceOptions) error {
	f.calls = append(f.calls, "service")
	return nil
}

func (f *fakeSSHRemote) StartTunnel(context.Context, sshtransport.TunnelOptions) (sshTunnelHandle, error) {
	f.calls = append(f.calls, "tunnel")
	return f.tunnel, nil
}

func (f *fakeSSHRemote) RunOutput(cmd string) (string, error) {
	f.calls = append(f.calls, "run:"+cmd)
	if f.runOut == "" {
		return "", errors.New("missing runOut")
	}
	return f.runOut, nil
}

func (f *fakeSSHRemote) ServiceStatus(sshtransport.Platform) (string, error) {
	f.calls = append(f.calls, "status")
	return "active\n", nil
}

func (f *fakeSSHRemote) RestartService(sshtransport.Platform) error {
	f.calls = append(f.calls, "restart")
	return nil
}

func (f *fakeSSHRemote) Teardown(sshtransport.Platform) error {
	f.calls = append(f.calls, "teardown")
	return nil
}

func (f *fakeSSHRemote) Close() error {
	f.calls = append(f.calls, "close")
	return nil
}

type fakeSSHTunnel struct {
	url string
}

func (f fakeSSHTunnel) URL() string {
	return f.url
}

func (f fakeSSHTunnel) Close() error {
	return nil
}
