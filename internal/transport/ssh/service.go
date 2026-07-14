package ssh

import (
	"fmt"
	"strings"
)

const (
	remoteSystemdUnit = "onibi.service"
	remoteLaunchdID   = "io.onibi"
)

type ServiceOptions struct {
	Transport string
}

func (c *Client) InstallService(platform Platform, opts ServiceOptions) error {
	if err := ValidatePlatform(platform); err != nil {
		return err
	}
	transport, err := serviceTransport(opts)
	if err != nil {
		return err
	}
	switch platform.GOOS {
	case "linux":
		return c.runRemote(systemdInstallCommand(transport))
	case "darwin":
		return c.runRemote(launchdInstallCommand(transport))
	default:
		return fmt.Errorf("ssh: unsupported service os: %s", platform.GOOS)
	}
}

func (c *Client) ServiceStatus(platform Platform) (string, error) {
	if err := ValidatePlatform(platform); err != nil {
		return "", err
	}
	switch platform.GOOS {
	case "linux":
		return c.RunOutput(systemdStatusCommand())
	case "darwin":
		return c.RunOutput(launchdStatusCommand())
	default:
		return "", fmt.Errorf("ssh: unsupported service os: %s", platform.GOOS)
	}
}

func (c *Client) RestartService(platform Platform) error {
	if err := ValidatePlatform(platform); err != nil {
		return err
	}
	switch platform.GOOS {
	case "linux":
		return c.runRemote("systemctl --user restart " + remoteSystemdUnit)
	case "darwin":
		return c.runRemote(`launchctl kickstart -k "gui/$(id -u)/` + remoteLaunchdID + `"`)
	default:
		return fmt.Errorf("ssh: unsupported service os: %s", platform.GOOS)
	}
}

func (c *Client) Teardown(platform Platform) error {
	if err := ValidatePlatform(platform); err != nil {
		return err
	}
	switch platform.GOOS {
	case "linux":
		return c.runRemote(systemdTeardownCommand())
	case "darwin":
		return c.runRemote(launchdTeardownCommand())
	default:
		return fmt.Errorf("ssh: unsupported service os: %s", platform.GOOS)
	}
}

func serviceTransport(opts ServiceOptions) (string, error) {
	transport := strings.TrimSpace(opts.Transport)
	if transport == "" {
		return "lan-loopback", nil
	}
	for _, r := range transport {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("ssh: invalid service transport: %q", transport)
		}
	}
	return transport, nil
}

func systemdInstallCommand(transport string) string {
	unit := `[Unit]
Description=Onibi SSH remote daemon
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/onibi up --transport=` + transport + ` --no-qr
Restart=on-failure
RestartSec=5
Environment=ONIBI_SERVICE=1

[Install]
WantedBy=default.target
`
	path := `"$HOME/.config/systemd/user/` + remoteSystemdUnit + `"`
	return strings.Join([]string{
		"set -eu",
		`mkdir -p "$HOME/.config/systemd/user"`,
		writeRemoteFileCommand(path, unit),
		"systemctl --user daemon-reload",
		"systemctl --user enable --now " + remoteSystemdUnit,
		"systemctl --user restart " + remoteSystemdUnit,
	}, "\n")
}

func launchdInstallCommand(transport string) string {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + remoteLaunchdID + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>-lc</string>
    <string>exec "$HOME/.local/bin/onibi" up --transport=` + transport + ` --no-qr</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>Crashed</key>
    <true/>
  </dict>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`
	path := `"$HOME/Library/LaunchAgents/` + remoteLaunchdID + `.plist"`
	return strings.Join([]string{
		"set -eu",
		`mkdir -p "$HOME/Library/LaunchAgents"`,
		writeRemoteFileCommand(path, plist),
		`launchctl bootout "gui/$(id -u)" ` + path + ` >/dev/null 2>&1 || true`,
		`launchctl bootstrap "gui/$(id -u)" ` + path,
		`launchctl kickstart -k "gui/$(id -u)/` + remoteLaunchdID + `"`,
	}, "\n")
}

func writeRemoteFileCommand(pathExpr, content string) string {
	return "cat > " + pathExpr + " <<'ONIBI_REMOTE_FILE'\n" + content + "ONIBI_REMOTE_FILE"
}

func systemdStatusCommand() string {
	return "systemctl --user status --no-pager --lines=0 " + remoteSystemdUnit
}

func launchdStatusCommand() string {
	return `launchctl print "gui/$(id -u)/` + remoteLaunchdID + `"`
}

func systemdTeardownCommand() string {
	return strings.Join([]string{
		"set -eu",
		"systemctl --user disable --now " + remoteSystemdUnit + " >/dev/null 2>&1 || true",
		`rm -f "$HOME/.config/systemd/user/` + remoteSystemdUnit + `"`,
		"systemctl --user daemon-reload >/dev/null 2>&1 || true",
		`rm -f "$HOME/.local/bin/onibi" "$HOME/.local/bin/onibi-notify"`,
	}, "\n")
}

func launchdTeardownCommand() string {
	return strings.Join([]string{
		"set -eu",
		`launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/` + remoteLaunchdID + `.plist" >/dev/null 2>&1 || true`,
		`rm -f "$HOME/Library/LaunchAgents/` + remoteLaunchdID + `.plist"`,
		`rm -f "$HOME/.local/bin/onibi" "$HOME/.local/bin/onibi-notify"`,
	}, "\n")
}
