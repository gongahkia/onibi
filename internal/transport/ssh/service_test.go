package ssh

import (
	"strings"
	"testing"
)

func TestSystemdInstallCommand(t *testing.T) {
	cmd := systemdInstallCommand("lan-loopback")
	for _, want := range []string{
		`cat > "$HOME/.config/systemd/user/onibi.service"`,
		`ExecStart=%h/.local/bin/onibi up --transport=lan-loopback --no-qr`,
		`systemctl --user daemon-reload`,
		`systemctl --user enable --now onibi.service`,
		`systemctl --user restart onibi.service`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("systemd command missing %q:\n%s", want, cmd)
		}
	}
}

func TestLaunchdInstallCommand(t *testing.T) {
	cmd := launchdInstallCommand("lan-loopback")
	for _, want := range []string{
		`cat > "$HOME/Library/LaunchAgents/io.onibi.plist"`,
		`<string>io.onibi</string>`,
		`exec "$HOME/.local/bin/onibi" up --transport=lan-loopback --no-qr`,
		`launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/io.onibi.plist"`,
		`launchctl kickstart -k "gui/$(id -u)/io.onibi"`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("launchd command missing %q:\n%s", want, cmd)
		}
	}
}

func TestServiceTransportDefault(t *testing.T) {
	got, err := serviceTransport(ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "lan-loopback" {
		t.Fatalf("transport = %q", got)
	}
	got, err = serviceTransport(ServiceOptions{Transport: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "lan" {
		t.Fatalf("transport = %q", got)
	}
	if _, err := serviceTransport(ServiceOptions{Transport: "lan;rm"}); err == nil {
		t.Fatal("expected invalid transport error")
	}
}

func TestServiceStatusAndTeardownCommands(t *testing.T) {
	for _, want := range []string{"systemctl --user status --no-pager --lines=0 onibi.service", "launchctl print"} {
		if got := systemdStatusCommand() + "\n" + launchdStatusCommand(); !strings.Contains(got, want) {
			t.Fatalf("status command missing %q", want)
		}
	}
	for _, want := range []string{"disable --now onibi.service", "rm -f \"$HOME/.local/bin/onibi\"", "LaunchAgents/io.onibi.plist"} {
		if got := systemdTeardownCommand() + "\n" + launchdTeardownCommand(); !strings.Contains(got, want) {
			t.Fatalf("teardown command missing %q", want)
		}
	}
}

func TestRestartServiceCommands(t *testing.T) {
	if got := "systemctl --user restart " + remoteSystemdUnit; !strings.Contains(got, "restart onibi.service") {
		t.Fatalf("linux restart command = %q", got)
	}
	if got := `launchctl kickstart -k "gui/$(id -u)/` + remoteLaunchdID + `"`; !strings.Contains(got, "kickstart -k") {
		t.Fatalf("macOS restart command = %q", got)
	}
}
